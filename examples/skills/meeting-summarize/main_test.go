package main

import (
	"encoding/json"
	"testing"
)

func TestExtractSummary(t *testing.T) {
	transcript := "Alice said: Let's start the meeting. We need to discuss the Q4 roadmap."
	summary := extractSummary(transcript)

	if summary == "" {
		t.Error("Expected non-empty summary")
	}
}

func TestExtractActionItems(t *testing.T) {
	text := `Here are the action items:
- Review the document
- Schedule the meeting
* Send the email`

	items := extractActionItems(text)

	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d: %v", len(items), items)
	}
}

func TestExtractAttendees(t *testing.T) {
	transcript := "Alice said: Let's start. Bob mentioned: We have blockers. Carol asked: What's the timeline?"

	attendees := extractAttendees(transcript)

	if len(attendees) != 3 {
		t.Errorf("Expected 3 attendees, got %d: %v", len(attendees), attendees)
	}
}

func TestRunSuccess(t *testing.T) {
	input := Input{
		Transcript: "Alice said: We need to review Q4. Bob asked: When is the deadline? - Review specs - Schedule follow-up",
	}
	data, _ := json.Marshal(input)
	output := run(data)

	var result Output
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}
	if result.Summary == "" {
		t.Error("Expected summary")
	}
}

func TestRunEmptyTranscript(t *testing.T) {
	input := Input{Transcript: ""}
	data, _ := json.Marshal(input)
	output := run(data)

	var result Output
	_ = json.Unmarshal(output, &result)

	if result.Error == "" {
		t.Error("Expected error for empty transcript")
	}
}

func TestRunInvalidInput(t *testing.T) {
	output := run([]byte(`not json`))

	var result Output
	_ = json.Unmarshal(output, &result)

	if result.Error == "" {
		t.Error("Expected error for invalid input")
	}
}

func TestMaxLengthDefault(t *testing.T) {
	input := Input{Transcript: "Test transcript"}
	data, _ := json.Marshal(input)
	output := run(data)

	var result Output
	_ = json.Unmarshal(output, &result)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}
}
