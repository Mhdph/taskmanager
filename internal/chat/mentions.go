package chat

import (
	"sort"
	"strings"
)

// Mention syntax is "@" followed by a member's user id. The user id is
// matched literally against the conversation members, so an id may contain
// characters that would be special in a regular expression.
//
// A mention only counts when it starts a word and is not glued to a longer
// name: "@alice" mentions alice, but "alice@example.com" and "@alice2" do
// not. A trailing dot is allowed ("thanks @alice." at the end of a
// sentence), while a dot followed by more name characters continues the
// token ("@alice.bob" is not a mention of alice).
func parseMentions(body string, userIDs []string) []string {
	if body == "" || len(userIDs) == 0 {
		return nil
	}

	candidates := uniqueUserIDs(userIDs)

	// Longest ids first: with members "alice" and "alice.bob", the text
	// "@alice.bob" is a mention of alice.bob, not of alice.
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i]) > len(candidates[j])
	})

	var (
		accepted  []mentionSpan
		mentioned = map[string]struct{}{}
		found     []struct {
			userID string
			at     int
		}
	)

	for _, userID := range candidates {
		token := "@" + userID

		offset := 0

		for {
			index := strings.Index(body[offset:], token)
			if index < 0 {
				break
			}

			start := offset + index
			end := start + len(token)
			offset = start + 1

			if !mentionHasBoundaries(body, start, end) {
				continue
			}

			if overlapsSpan(accepted, start, end) {
				continue
			}

			// The same member is listed once even when mentioned twice.
			if _, ok := mentioned[userID]; ok {
				continue
			}

			mentioned[userID] = struct{}{}

			accepted = append(
				accepted,
				mentionSpan{start: start, end: end},
			)

			found = append(found, struct {
				userID string
				at     int
			}{userID: userID, at: start})
		}
	}

	if len(found) == 0 {
		return nil
	}

	sort.SliceStable(found, func(i, j int) bool {
		return found[i].at < found[j].at
	})

	mentions := make([]string, 0, len(found))
	for _, item := range found {
		mentions = append(mentions, item.userID)
	}

	return mentions
}

func uniqueUserIDs(userIDs []string) []string {
	seen := make(map[string]struct{}, len(userIDs))
	unique := make([]string, 0, len(userIDs))

	for _, userID := range userIDs {
		if userID == "" {
			continue
		}

		if _, ok := seen[userID]; ok {
			continue
		}

		seen[userID] = struct{}{}
		unique = append(unique, userID)
	}

	return unique
}

type mentionSpan struct {
	start int
	end   int
}

func overlapsSpan(
	spans []mentionSpan,
	start int,
	end int,
) bool {
	for _, existing := range spans {
		if start < existing.end && existing.start < end {
			return true
		}
	}

	return false
}

// mentionHasBoundaries reports whether the token at [start,end) is a
// standalone mention.
func mentionHasBoundaries(body string, start int, end int) bool {
	if start > 0 && isMentionIdentifierChar(body[start-1]) {
		return false
	}

	if end >= len(body) {
		return true
	}

	next := body[end]

	if isMentionIdentifierChar(next) {
		return false
	}

	// A dot only continues the token when a name follows it.
	if next == '.' && end+1 < len(body) && isMentionNameChar(body[end+1]) {
		return false
	}

	return true
}

func isMentionIdentifierChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-' || c == '+':
		return true
	default:
		return false
	}
}

func isMentionNameChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	default:
		return false
	}
}
