package aptpackage

import (
	"context"
	"strings"
	"testing"
)

func TestHostedPublicationRejectsAmbiguousDestinationsAndTrust(t *testing.T) {
	for _, target := range []string{"ssh://host/path", "host:path", "http://host/repo", "tparun+ssh://root@host/" + strings.Repeat("a", 32), "tparun+ssh://host/not-a-repository", "tparun+ssh://host/" + strings.Repeat("a", 32) + "/extra"} {
		if _, err := PublishRepositoryToTPARunSSH(context.Background(), t.TempDir(), target, HostedPublishOptions{}); err == nil {
			t.Fatalf("accepted unsupported destination %s", target)
		}
	}
	if _, err := PublishRepositoryToTPARunSSH(context.Background(), t.TempDir(), "tparun+ssh://host/"+strings.Repeat("a", 32), HostedPublishOptions{}); err == nil || !strings.Contains(err.Error(), "public keyring") {
		t.Fatalf("missing trust accepted: %v", err)
	}
}
