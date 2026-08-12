package migrator

import "testing"

func TestMapFolderName(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		srcDelim byte
		dstDelim byte
		want     string
	}{
		{
			name:     "same delimiter, no change",
			input:    "INBOX.Work.2024",
			srcDelim: '.',
			dstDelim: '.',
			want:     "INBOX.Work.2024",
		},
		{
			name:     "dot to slash",
			input:    "INBOX.Work.2024",
			srcDelim: '.',
			dstDelim: '/',
			want:     "INBOX/Work/2024",
		},
		{
			name:     "slash to dot",
			input:    "INBOX/Archive/2023",
			srcDelim: '/',
			dstDelim: '.',
			want:     "INBOX.Archive.2023",
		},
		{
			name:     "top level folder, no delimiter present",
			input:    "INBOX",
			srcDelim: '.',
			dstDelim: '/',
			want:     "INBOX",
		},
		{
			name:     "zero srcDelim means unknown, leave untouched",
			input:    "INBOX.Sent",
			srcDelim: 0,
			dstDelim: '/',
			want:     "INBOX.Sent",
		},
		{
			name:     "empty name",
			input:    "",
			srcDelim: '.',
			dstDelim: '/',
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapFolderName(tc.input, tc.srcDelim, tc.dstDelim)
			if got != tc.want {
				t.Errorf("MapFolderName(%q, %q, %q) = %q, want %q",
					tc.input, tc.srcDelim, tc.dstDelim, got, tc.want)
			}
		})
	}
}
