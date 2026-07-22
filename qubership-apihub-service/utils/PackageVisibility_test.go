package utils

import "testing"

func TestCompressVisibleRoots(t *testing.T) {
	got := CompressVisibleRoots([]string{"ws", "ws.a", "ws.b"})
	if len(got) != 1 || got[0] != "ws" {
		t.Fatalf("expected [ws], got %v", got)
	}
	got = CompressVisibleRoots([]string{"ws.a", "ws.b"})
	if len(got) != 2 {
		t.Fatalf("expected two roots, got %v", got)
	}
	got = CompressVisibleRoots([]string{"ws.shared"})
	if len(got) != 1 || got[0] != "ws.shared" {
		t.Fatalf("expected [ws.shared], got %v", got)
	}
}

func TestCompressInvisibleRoots(t *testing.T) {
	all := []string{"ws", "ws.public", "ws.secret", "ws.secret.child"}
	readable := []string{"ws", "ws.public"}
	got := CompressInvisibleRoots(all, readable)
	if len(got) != 1 || got[0] != "ws.secret" {
		t.Fatalf("expected [ws.secret], got %v", got)
	}

	// private workspace with visible child: no invisible subtree root
	all = []string{"ws", "ws.shared", "ws.other"}
	readable = []string{"ws.shared"}
	got = CompressInvisibleRoots(all, readable)
	if len(got) != 1 || got[0] != "ws.other" {
		t.Fatalf("expected [ws.other], got %v", got)
	}
}

func TestPartitionSlug(t *testing.T) {
	slug := PartitionSlug("acme-corp")
	if len(slug) != 18 || slug[:2] != "p_" {
		t.Fatalf("unexpected slug %q", slug)
	}
	if PartitionSlug("acme-corp") != slug {
		t.Fatal("slug must be deterministic")
	}
}
