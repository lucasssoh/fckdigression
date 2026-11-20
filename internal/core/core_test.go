package core

import "testing"

func TestParseStatusV2(t *testing.T) {
	s := ParseStatus("app_version=2 owner=true dns_mode=hostname dns=dns.example.com lock_dns=true lock_users=true lock_reset=false lock_vpn=true lock_debug=false code_set=true code_len=30 fails=2 wait_s=0")
	if !s.Locked() || s.AppVersion != 2 || s.DNS != "dns.example.com" || s.DNSMode != "hostname" {
		t.Fatalf("bad parse: %+v", s)
	}
	if !s.LockVPN || s.LockReset || s.LockDebug || !s.LockUsers || !s.UsersKnown {
		t.Fatalf("bad flags: %+v", s)
	}
	if !s.CodeSet || s.CodeLen != 30 || s.Fails != 2 {
		t.Fatalf("bad code: %+v", s)
	}
}

func TestParseStatusV1French(t *testing.T) {
	s := ParseStatus("proprietaire=true dns_mode=serveur dns=dns.example.com verrou_dns=true verrou_reset=true verrou_vpn=true verrou_debug=false code_defini=true")
	if s.AppVersion != 1 || !s.Owner || !s.Locked() || s.DNSMode != "hostname" || !s.LockReset || !s.CodeSet {
		t.Fatalf("bad v1 parse: %+v", s)
	}
	if s.UsersKnown || s.CodeLen != 0 {
		t.Fatalf("v1 has no users/len: %+v", s)
	}
}

func TestCheckCode(t *testing.T) {
	for code, ok := range map[string]bool{"123456": true, "12345": false, "12a456": false, "": false, GenerateCode(30): true} {
		if (CheckCode(code) == nil) != ok {
			t.Errorf("CheckCode(%q) ok=%v", code, !ok)
		}
	}
	if g := GenerateCode(30); len(g) != 30 {
		t.Errorf("GenerateCode length %d", len(g))
	}
	if Group("123456789012") != "12345 67890 12" {
		t.Errorf("Group: %q", Group("123456789012"))
	}
}

func TestValidHost(t *testing.T) {
	for h, ok := range map[string]bool{"dns.example.com": true, "dns.home.arpa": true, "bad host": false, "-x.com": false, "localhost": false} {
		if ValidHost(h) != ok {
			t.Errorf("ValidHost(%q) != %v", h, ok)
		}
	}
}
