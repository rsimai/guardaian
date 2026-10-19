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
const defaultThreshold = 0.75
const defaultURL = "http://localhost:11434"

type config struct {
	Model     string
	Threshold float64
	URL       string
	Timeout   time.Duration
	CheckOnly bool
}

type ollamaResponse struct {
	Response string `json:"response"`
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

	if score < cfg.Threshold {
		fmt.Println("Command allowed by policy.")
		if cfg.CheckOnly {
			return
		}
		if err := runCommand(command); err != nil {
			fmt.Fprintf(os.Stderr, "command failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if !confirmProceed(score, cfg.Threshold) {
		fmt.Println("Execution blocked.")
		os.Exit(1)
	}
	if cfg.CheckOnly {
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
			"dangerous": map[string]any{
				"type":         "noul",
				"instructions": "Does this command appear dangerous, destructive, or likely to cause harm?",
				"criteria": map[string]string{
					"true":  "The command is dangerous or destructive.",
					"false": "The command is benign or low risk.",
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

	score, ok := extractSystemOneScore(result.Answers)
	if !ok {
		return 0, fmt.Errorf("model response did not include a noul answer")
	}
	if score < 0 || score > 1 {
		return 0, fmt.Errorf("model returned an invalid score %v", score)
	}
	return score, nil
}

func extractSystemOneScore(answers map[string]map[string]any) (float64, bool) {
	for _, answer := range answers {
		if value, ok := answer["noul"]; ok {
			switch v := value.(type) {
			case float64:
				return v, true
			case float32:
				return float64(v), true
			case int:
				return float64(v), true
			case string:
				parsed, err := strconv.ParseFloat(v, 64)
				if err == nil {
					return parsed, true
				}
			}
		}
	}
	return 0, false
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
						if score, ok := extractSystemOneScore(mapify(answerMap)); ok {
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

func mapify(input map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			out[key] = nested
		}
	}
	return out
}

func shouldAskForConfirmation(score, threshold float64) bool {
	return score >= threshold
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
