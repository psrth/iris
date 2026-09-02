package tunnel

import (
	"testing"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"
)

func TestToken(t *testing.T) {
	ci := tailcat.ConnInfo{
		ServerPublic:      tailcat.NodePublic{NodePublic: key.NewNode().Public()},
		ServerDiscoPublic: tailcat.DiscoPublicForNode(key.NewNode()),
		RegionID:          302,
	}
	want := Token{Blob: ci.ConnBlob(), UID: "b70b5992da99e182", Key: "hVZq9O77ucUH12w67qfHGsTn6gZBm5Qa9nkYUUD9dlY"}
	got, err := Parse(" " + want.String() + "\n")
	if err != nil || got != want {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	for _, bad := range []string{
		"",
		"tcabc",
		"tcabc.uid",
		"nope.uid.key",
		"tcabc..key",
		want.String() + ".extra",
		"tc!!!.uid.key",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}
