package chat

import (
	"reflect"
	"testing"
)

func TestParseMentions(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		member []string
		want   []string
	}{
		{
			name:   "simple mention",
			body:   "@alice can you take a look?",
			member: []string{"alice", "bob"},
			want:   []string{"alice"},
		},
		{
			name:   "mention with trailing comma",
			body:   "hey @bob, ship it",
			member: []string{"alice", "bob"},
			want:   []string{"bob"},
		},
		{
			name:   "mention at end of sentence",
			body:   "thanks @alice.",
			member: []string{"alice"},
			want:   []string{"alice"},
		},
		{
			name:   "mention in parentheses",
			body:   "(@alice) please review",
			member: []string{"alice"},
			want:   []string{"alice"},
		},
		{
			name:   "two mentions keep body order",
			body:   "@bob and @alice should pair up",
			member: []string{"alice", "bob"},
			want:   []string{"bob", "alice"},
		},
		{
			name:   "repeated mention is listed once",
			body:   "@alice ping, @alice again",
			member: []string{"alice"},
			want:   []string{"alice"},
		},
		{
			name:   "longer identifier wins",
			body:   "@alice.bob look",
			member: []string{"alice", "alice.bob"},
			want:   []string{"alice.bob"},
		},
		{
			name:   "dotted suffix is not a mention",
			body:   "@alice.bob look",
			member: []string{"alice"},
			want:   nil,
		},
		{
			name:   "email is not a mention",
			body:   "mail alice@example.com about it",
			member: []string{"alice", "example.com"},
			want:   nil,
		},
		{
			name:   "longer word is not a mention",
			body:   "@alice2 look",
			member: []string{"alice"},
			want:   nil,
		},
		{
			name:   "underscore continues the token",
			body:   "@alice_bob look",
			member: []string{"alice"},
			want:   nil,
		},
		{
			name:   "underscore identifier is matched",
			body:   "@alice_bob look",
			member: []string{"alice", "alice_bob"},
			want:   []string{"alice_bob"},
		},
		{
			name:   "dotted identifier is matched",
			body:   "cc @john.doe",
			member: []string{"john.doe", "alice"},
			want:   []string{"john.doe"},
		},
		{
			name:   "non member is ignored",
			body:   "@carol is not here",
			member: []string{"alice", "bob"},
			want:   nil,
		},
		{
			name:   "empty body",
			body:   "",
			member: []string{"alice"},
			want:   nil,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseMentions(testCase.body, testCase.member)

			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf(
					"parseMentions(%q) = %v, want %v",
					testCase.body,
					got,
					testCase.want,
				)
			}
		})
	}
}
