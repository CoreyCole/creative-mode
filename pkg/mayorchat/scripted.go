package mayorchat

import (
	"fmt"
	"strings"
)

// ScriptedResponses contains pre-written responses for each stage when the API is unavailable.
// Stage is determined by counting user messages (0-indexed).
var ScriptedResponses = []string{
	// Stage 0: user described their world idea
	"Okay, I can work with that. What would people actually *do* there — " +
		"chill hang-out-and-explore, or more of a quest-and-fight-things situation?",
	// Stage 1: user described mood/gameplay — suggest world names
	"That's starting to feel like a real place. What do you want to call it? " +
		"A few ideas off the top of my head: **Duskhollow**, **Ember Reach**, **Wanderveil** — " +
		"or something totally different. Your call.",
	// Stage 2: user provided world name — confirm it, then ask for mayor name
	"**%s** — yeah, that works. Now — I need a name too. " +
		"I'm the mayor of this place, so pick something that fits. What should I go by?",
	// Stage 3: user provided mayor name (response uses placeholders, triggers hatchWorld)
	"**%s**, mayor of **%s**. I like the sound of that. Let's get this place built.",
}

// ScriptedNameRefusalResponse is shown when the user declines to name the mayor.
const ScriptedNameRefusalResponse = "Just \"Mayor\"? That's... functional. " +
	"A bit like naming your dog \"Dog.\" But hey, it works — you can rename me any time. " +
	"What should I go by for real, or should I just roll with **Mayor**?"

// ScriptedNameRefusalConfirmResponse is shown after the user confirms "Mayor" a second time.
const ScriptedNameRefusalConfirmResponse = "**Mayor** of **%s**. Fine, I'll own it. Let's get this place built."

// IsMayorNameRefusal returns true if the user's input looks like a refusal to
// name the mayor (empty, vague, or literally "mayor").
func IsMayorNameRefusal(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return true
	}
	refusals := []string{
		"mayor", "just mayor", "idk", "i don't know", "i dont know",
		"no", "nah", "no idea", "dunno", "whatever", "skip",
		"no name", "none", "pass", "default",
	}
	for _, r := range refusals {
		if normalized == r {
			return true
		}
	}
	return false
}

// ScriptedStage returns the conversation stage based on user message count.
func ScriptedStage(messages []Message) int {
	stage := CountUserMessages(messages) - 1
	if stage < 0 {
		stage = 0
	}
	return stage
}

// ScriptedResponseForStage returns the pre-written response for the given stage,
// substituting names from conversation history where needed.
func ScriptedResponseForStage(stage int, messages []Message) string {
	if stage >= len(ScriptedResponses) {
		stage = len(ScriptedResponses) - 1
	}

	switch stage {
	case 2:
		worldName := NthUserMessage(messages, 2)
		return fmt.Sprintf(ScriptedResponses[stage], worldName)
	case 3:
		mayorName := LastUserMessage(messages)
		worldName := NthUserMessage(messages, 2)
		return fmt.Sprintf(ScriptedResponses[stage], mayorName, worldName)
	default:
		return ScriptedResponses[stage]
	}
}

// ScriptedExtractWorldInfo extracts world info from a scripted conversation.
func ScriptedExtractWorldInfo(messages []Message, stage int) (mayorName, worldName, worldSummary string) {
	if stage >= 3 {
		mayorName = LastUserMessage(messages)
		worldName = NthUserMessage(messages, 2)
		worldSummary = Truncate(NthUserMessage(messages, 0), 100)
		if IsMayorNameRefusal(mayorName) {
			mayorName = "Mayor"
		}
	}
	return mayorName, worldName, worldSummary
}
