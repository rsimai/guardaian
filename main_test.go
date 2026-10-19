package main

import "testing"

func TestExtractScoreFromResponse(t *testing.T) {
	resp := `{"answers":{"dangerous":{"noul":0.82}}}`
	got := extractScore(resp)
	if got != 0.82 {
		t.Fatalf("extractScore() = %v, want 0.82", got)
	}
}

func TestExtractScoreFromNonJSONText(t *testing.T) {
	resp := "Risk score: 0.34"
	got := extractScore(resp)
	if got != 0.34 {
		t.Fatalf("extractScore() = %v, want 0.34", got)
	}
}

func TestShouldAskForConfirmation(t *testing.T) {
	if !shouldAskForConfirmation(0.9, 0.7) {
		t.Fatal("expected confirmation when score exceeds threshold")
	}
	if shouldAskForConfirmation(0.4, 0.7) {
		t.Fatal("did not expect confirmation when score stays below threshold")
	}
}
