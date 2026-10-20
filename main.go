package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const defaultModel = "tev1:0.8b"
const defaultThreshold = 0.5
const defaultURL = "http://localhost:11434"
const exitCodeConfirmationRequired = 20

type config struct {
	Model     string
	Threshold float64
	URL       string
	Timeout   time.Duration
	CheckOnly bool
}

func main() {
	cfg := config{
		Model:     defaultModel,
		Threshold: defaultThreshold,
		URL:       defaultURL,
		Timeout:   30 * time.Second,
	}
	if v := os.Getenv("GUARDAIAN_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("GUARDAIAN_THRESHOLD"); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err == nil {
			cfg.Threshold = parsed
		}
	}
	if v := os.Getenv("GUARDAIAN_OLLAMA_URL"); v != "" {
		cfg.URL = v
	}

	flag.StringVar(&cfg.Model, "model", cfg.Model, "Ollama model to query")
	flag.Float64Var(&cfg.Threshold, "threshold", cfg.Threshold, "decision threshold (0.0-1.0)")
	flag.StringVar(&cfg.URL, "url", cfg.URL, "base URL for the Ollama API")
	flag.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP timeout for the model request")
	flag.BoolVar(&cfg.CheckOnly, "check-only", false, "evaluate a command without executing it")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] -- <command>\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Evaluate the command risk with a local Ollama model before running it.")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	command := strings.Join(args, " ")
	score, err := evaluateRisk(cfg, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "risk evaluation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Risk score: %.2f (threshold %.2f)\n", score, cfg.Threshold)

	if !shouldAskForConfirmation(score, cfg.Threshold) {
		fmt.Println("Command allowed by policy.")
		if !shouldExecuteCommand(cfg.CheckOnly, score, cfg.Threshold, true) {
			return
		}
		if err := runCommand(command); err != nil {
			fmt.Fprintf(os.Stderr, "command failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if cfg.CheckOnly {
		fmt.Println("Confirmation required.")
		os.Exit(exitCodeConfirmationRequired)
	}

	confirmed := confirmProceed(score, cfg.Threshold)
	if !confirmed {
		fmt.Println("Execution blocked.")
		os.Exit(1)
	}
	if !shouldExecuteCommand(cfg.CheckOnly, score, cfg.Threshold, confirmed) {
		return
	}

	if err := runCommand(command); err != nil {
		fmt.Fprintf(os.Stderr, "command failed: %v\n", err)
		os.Exit(1)
	}
}

func evaluateRisk(cfg config, command string) (float64, error) {
	payload := map[string]any{
		"model": cfg.Model,
		"state": command,
		"questions": map[string]any{
			"modifies_filesystem": map[string]any{
				"type":         "noul",
				"instructions": "Does this command modify filesystem state directly or indirectly? Include redirections, in-place flags, and invoked tools/scripts. Pure read/list/inspect commands should be false.",
				"criteria": map[string]string{
					"true":  "The command can create, modify, overwrite, append, truncate, rename, delete, or change metadata/permissions.",
					"false": "The command is strictly read-only and does not alter filesystem data or metadata (examples: ls, cat, head, tail, grep without -i in-place edits).",
				},
			},
			"destructive_or_hard_to_rollback": map[string]any{
				"type":         "noul",
				"instructions": "Can this command cause irreversible loss, broad disruption, or difficult rollback? Base this on actual side effects. If the command is clearly read-only, this should be false.",
				"criteria": map[string]string{
					"true":  "Potential for data loss, broad deletion, service disruption, security-impacting changes, or difficult recovery.",
					"false": "No meaningful destructive impact and easy rollback.",
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	request, err := http.NewRequest(http.MethodPost, cfg.URL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := (&http.Client{Timeout: cfg.Timeout}).Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return 0, fmt.Errorf("ollama returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Answers map[string]map[string]any `json:"answers"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return 0, err
	}

	writeScore, ok := extractNamedSystemOneScore(result.Answers, "modifies_filesystem")
	if !ok {
		return 0, fmt.Errorf("model response did not include modifies_filesystem noul answer")
	}
	destructiveScore, ok := extractNamedSystemOneScore(result.Answers, "destructive_or_hard_to_rollback")
	if !ok {
		return 0, fmt.Errorf("model response did not include destructive_or_hard_to_rollback noul answer")
	}
	if writeScore < 0 || writeScore > 1 {
		return 0, fmt.Errorf("model returned an invalid modifies_filesystem score %v", writeScore)
	}
	if destructiveScore < 0 || destructiveScore > 1 {
		return 0, fmt.Errorf("model returned an invalid destructive_or_hard_to_rollback score %v", destructiveScore)
	}

	return combineRisk(writeScore, destructiveScore), nil
}

func extractNamedSystemOneScore(answers map[string]map[string]any, key string) (float64, bool) {
	answer, ok := answers[key]
	if !ok {
		return 0, false
	}

	return extractNoulFromAnswerMap(answer)
}

func combineRisk(modifiesFilesystem, destructive float64) float64 {
	risk := modifiesFilesystem * (1 + 0.5*destructive)
	if risk > 1 {
		return 1
	}
	return risk
}

func extractNumberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func extractScore(raw string) float64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if value, ok := payload["score"]; ok {
			if score, ok := extractNumberFromAny(value); ok && score >= 0 && score <= 1 {
				return score
			}
		}
		if value, ok := payload["noul"]; ok {
			if score, ok := extractNumberFromAny(value); ok && score >= 0 && score <= 1 {
				return score
			}
		}
		if answersValue, ok := payload["answers"]; ok {
			if answers, ok := answersValue.(map[string]any); ok {
				for _, answer := range answers {
					if answerMap, ok := answer.(map[string]any); ok {
						if score, ok := extractNoulFromAnswerMap(answerMap); ok {
							return score
						}
					}
				}
			}
		}
	}

	for _, candidate := range strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ':' || r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '{' || r == '}' || r == '[' || r == ']'
	}) {
		value, err := strconv.ParseFloat(candidate, 64)
		if err != nil {
			continue
		}
		if value >= 0 && value <= 1 {
			return value
		}
	}

	return 0
}

func extractNoulFromAnswerMap(answer map[string]any) (float64, bool) {
	value, ok := answer["noul"]
	if !ok {
		return 0, false
	}
	return extractNumberFromAny(value)
}

func shouldAskForConfirmation(score, threshold float64) bool {
	return score >= threshold
}

func shouldExecuteCommand(checkOnly bool, score, threshold float64, confirmed bool) bool {
	if score < threshold {
		return !checkOnly
	}
	if checkOnly {
		return false
	}
	return confirmed
}

func confirmProceed(score, threshold float64) bool {
	fmt.Printf("Warning: score %.2f is at or above the configured threshold %.2f.\n", score, threshold)
	fmt.Print("Type \"yes\" to continue: ")

	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "unable to read confirmation: %v\n", err)
		return false
	}
	return strings.EqualFold(strings.TrimSpace(text), "yes")
}

func runCommand(command string) error {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
