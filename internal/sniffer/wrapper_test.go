package sniffer

import (
	"encoding/base64"
	"testing"
)

func TestUnwrapTargetURL(t *testing.T) {
	t.Parallel()
	inner := "https://bfq.txnp.cn/player?url=https%3A%2F%2Fv.youku.com%2Fv_show%2Fid_XNDQ3MzU2MzE2.html"
	encoded := base64.StdEncoding.EncodeToString([]byte(inner))

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ssmhd player wrapper",
			in:   "https://ssmhd.com/player/?url=" + encoded,
			want: inner,
		},
		{
			name: "without trailing slash",
			in:   "https://www.ssmhd.com/player?url=" + encoded,
			want: inner,
		},
		{
			name: "nested wrapper",
			in:   "https://ssmhd.com/player/?url=" + base64.StdEncoding.EncodeToString([]byte("https://ssmhd.com/player/?url="+encoded)),
			want: inner,
		},
		{
			name: "plain page untouched",
			in:   "https://v.youku.com/v_show/id_XNDQ3MzU2MzE2.html",
			want: "https://v.youku.com/v_show/id_XNDQ3MzU2MzE2.html",
		},
		{
			name: "other host untouched",
			in:   "https://example.com/player/?url=" + encoded,
			want: "https://example.com/player/?url=" + encoded,
		},
		{
			name: "invalid base64 untouched",
			in:   "https://ssmhd.com/player/?url=not-base64!!",
			want: "https://ssmhd.com/player/?url=not-base64!!",
		},
		{
			name: "decoded value must be a URL",
			in:   "https://ssmhd.com/player/?url=" + base64.StdEncoding.EncodeToString([]byte("hello")),
			want: "https://ssmhd.com/player/?url=" + base64.StdEncoding.EncodeToString([]byte("hello")),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := unwrapTargetURL(test.in); got != test.want {
				t.Fatalf("unwrapTargetURL(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestDecodeBase64RestoresPlus(t *testing.T) {
	t.Parallel()
	// A literal '+' turns into a space when parsed from a query string.
	withPlus, ok := decodeBase64("+/8=")
	if !ok {
		t.Fatal("failed to decode standard base64")
	}
	withSpace, ok := decodeBase64(" /8=")
	if !ok {
		t.Fatal("failed to decode base64 with a space")
	}
	if withPlus != withSpace {
		t.Fatalf("decoded %q and %q differ", withPlus, withSpace)
	}
}
