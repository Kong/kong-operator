package generator

import "strings"

// WrapLine wraps a line to the specified width, breaking at word boundaries.
// It first splits on sentence boundaries (". "), then wraps any long sentences.
func WrapLine(line string, maxWidth int) []string {
	if len(line) <= maxWidth {
		return []string{line}
	}

	var result []string

	// Split on sentence boundaries (". " followed by more text)
	sentences := SplitSentences(line)

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		// If sentence fits on one line, add it directly
		if len(sentence) <= maxWidth {
			result = append(result, sentence)
			continue
		}

		// Otherwise, wrap the sentence at word boundaries
		wrapped := WrapLongLine(sentence, maxWidth)
		result = append(result, wrapped...)
	}

	return result
}

// SplitSentences splits text on sentence boundaries (". " followed by more text)
func SplitSentences(text string) []string {
	var sentences []string
	remaining := text

	for {
		// Find ". " followed by a character (sentence boundary)
		idx := strings.Index(remaining, ". ")
		if idx == -1 {
			// No more sentence boundaries
			if remaining != "" {
				sentences = append(sentences, remaining)
			}
			break
		}

		// Check if there's more text after ". "
		if idx+2 < len(remaining) {
			// Add the sentence including the period
			sentences = append(sentences, remaining[:idx+1])
			remaining = remaining[idx+2:]
		} else {
			// ". " is at the end, just add the rest
			sentences = append(sentences, remaining)
			break
		}
	}

	return sentences
}

// WrapLongLine wraps a single long line at word boundaries
func WrapLongLine(line string, maxWidth int) []string {
	var result []string
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{line}
	}

	currentLine := words[0]
	for i := 1; i < len(words); i++ {
		word := words[i]
		tentativeLine := currentLine + " " + word

		if len(tentativeLine) > maxWidth {
			result = append(result, currentLine)
			currentLine = word
		} else {
			currentLine = tentativeLine
		}
	}

	if currentLine != "" {
		result = append(result, currentLine)
	}

	return result
}
