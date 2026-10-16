package aptpackage

import (
	"slices"
	"testing"
)

func TestCapabilitiesDescribeClientSignedSSHWithoutClaimingCASOrServer(t *testing.T) {
	c := GetCapabilities()
	if !slices.Contains(c.Operations, "repository.publish-client-signed-tparun-ssh.v1") {
		t.Fatal("missing finished-tree SSH operation")
	}
	p := c.Publication
	if !p.ClientSignedSSH || p.ClientSignedSSHProtocol != "tparun-prebuilt-ssh" || p.ClientSignedSSHProtocolVersion != 1 {
		t.Fatalf("wrong SSH client capability: %+v", p)
	}
	if p.HostedManagedPublication || p.ClientSignedSSHChunkedCAS || c.Transport.InboundSSHUpload || c.Transport.AuthenticatedUpload {
		t.Fatal("claimed unmanaged server, managed signing or unfinished CAS")
	}
	if len(c.OutputBackends) != 2 || c.OutputBackends[1].Name != "ssh-sftp" || !c.OutputBackends[1].NewDestinationOnly {
		t.Fatal("generic SFTP pack destination semantics changed")
	}
}
