package main

import (
	"encoding/json"
	"testing"
)

func TestEmptyInput(t *testing.T) {
	output := run(nil)

	var result SkillOutput
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if result.Message != "Hello! I'm the hello-world skill. How can I help you?" {
		t.Errorf("Unexpected response: %s", result.Message)
	}
}

func TestHello(t *testing.T) {
	input := SkillInput{Message: "hello there!"}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Message != "Hello! I'm the hello-world skill. How can I help you?" {
		t.Errorf("Unexpected response: %s", result.Message)
	}
}

func TestHi(t *testing.T) {
	input := SkillInput{Message: "Hi"}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Message != "Hello! I'm the hello-world skill. How can I help you?" {
		t.Errorf("Unexpected response: %s", result.Message)
	}
}

func TestName(t *testing.T) {
	input := SkillInput{Message: "What's your name?"}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Message != "I'm the OpenBotStack hello-world skill!" {
		t.Errorf("Unexpected response: %s", result.Message)
	}
}

func TestHelp(t *testing.T) {
	input := SkillInput{Message: "help"}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Message != "I can respond to greetings. Try saying 'hello' or ask my name!" {
		t.Errorf("Unexpected response: %s", result.Message)
	}
}

func TestVersion(t *testing.T) {
	input := SkillInput{Message: "version"}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Message != "hello-world skill v1.0.0" {
		t.Errorf("Unexpected response: %s", result.Message)
	}
}

func TestUnknown(t *testing.T) {
	input := SkillInput{Message: "random message"}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Message != "I heard you say: random message" {
		t.Errorf("Unexpected response: %s", result.Message)
	}
}

func TestOutputMetadata(t *testing.T) {
	input := SkillInput{Message: "hi"}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Data["skill"] != "hello-world" {
		t.Errorf("Expected skill='hello-world', got %q", result.Data["skill"])
	}
	if result.Data["version"] != "1.0.0" {
		t.Errorf("Expected version='1.0.0', got %q", result.Data["version"])
	}
}

func TestInputWithSession(t *testing.T) {
	input := SkillInput{
		SessionID: "sess-123",
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Message:   "hello",
	}
	data, _ := json.Marshal(input)

	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Error != "" {
		t.Errorf("Unexpected error: %s", result.Error)
	}
}

func TestInvalidJSON(t *testing.T) {
	output := run([]byte("not json"))

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Error == "" {
		t.Error("Expected error for invalid JSON")
	}
}
