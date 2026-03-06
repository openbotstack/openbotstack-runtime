package main

import (
	"encoding/json"
	"testing"
)

func TestExecuteEmptyInput(t *testing.T) {
	inputBuffer = nil
	outputBuffer = nil

	err := Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var output SkillOutput
	_ = json.Unmarshal(outputBuffer, &output)

	if output.Message != "Hello! I'm the hello-world skill. How can I help you?" {
		t.Errorf("Unexpected response: %s", output.Message)
	}
}

func TestExecuteHello(t *testing.T) {
	input := SkillInput{Message: "hello there!"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Message != "Hello! I'm the hello-world skill. How can I help you?" {
		t.Errorf("Unexpected response: %s", output.Message)
	}
}

func TestExecuteHi(t *testing.T) {
	input := SkillInput{Message: "Hi"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Message != "Hello! I'm the hello-world skill. How can I help you?" {
		t.Errorf("Unexpected response: %s", output.Message)
	}
}

func TestExecuteName(t *testing.T) {
	input := SkillInput{Message: "What's your name?"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Message != "I'm the OpenBotStack hello-world skill!" {
		t.Errorf("Unexpected response: %s", output.Message)
	}
}

func TestExecuteHelp(t *testing.T) {
	input := SkillInput{Message: "help"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Message != "I can respond to greetings. Try saying 'hello' or ask my name!" {
		t.Errorf("Unexpected response: %s", output.Message)
	}
}

func TestExecuteVersion(t *testing.T) {
	input := SkillInput{Message: "version"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Message != "hello-world skill v1.0.0" {
		t.Errorf("Unexpected response: %s", output.Message)
	}
}

func TestExecuteUnknown(t *testing.T) {
	input := SkillInput{Message: "random message"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Message != "I heard you say: random message" {
		t.Errorf("Unexpected response: %s", output.Message)
	}
}

func TestOutputMetadata(t *testing.T) {
	input := SkillInput{Message: "hi"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Data["skill"] != "hello-world" {
		t.Errorf("Expected skill='hello-world', got %q", output.Data["skill"])
	}
	if output.Data["version"] != "1.0.0" {
		t.Errorf("Expected version='1.0.0', got %q", output.Data["version"])
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
	SetInput(data)

	Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Error != "" {
		t.Errorf("Unexpected error: %s", output.Error)
	}
}
