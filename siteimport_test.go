package main

import (
	"encoding/base64"
	"testing"
)

const fzSample = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<FileZilla3 version="3.66.5" platform="windows">
	<Servers>
		<Server>
			<Host>box.example.net</Host>
			<Port>22</Port>
			<Protocol>1</Protocol>
			<Type>0</Type>
			<User>seed</User>
			<Pass encoding="base64">` + "aHVudGVyMg==" + `</Pass>
			<Logontype>1</Logontype>
			<Name>Seedbox</Name>
			<RemoteDir>1 0 4 home 4 seed</RemoteDir>
		</Server>
		<Folder expanded="1">Work
			<Server>
				<Host>ftp.example.org</Host>
				<Port>990</Port>
				<Protocol>3</Protocol>
				<User>me</User>
				<Logontype>1</Logontype>
				<Name>Files</Name>
			</Server>
			<Folder>Old
				<Server>
					<Host>keys.example.org</Host>
					<Protocol>1</Protocol>
					<User>deploy</User>
					<Logontype>5</Logontype>
					<Keyfile>C:\keys\id_ed25519</Keyfile>
					<Name>Deploy</Name>
				</Server>
				<Server>
					<Host>dav.example.org</Host>
					<Protocol>5</Protocol>
					<User>x</User>
					<Name>WebDAV</Name>
				</Server>
			</Folder>
		</Folder>
		<Server>
			<Host>plain.example.org</Host>
			<Protocol>6</Protocol>
			<Logontype>0</Logontype>
			<Name></Name>
		</Server>
	</Servers>
</FileZilla3>`

func TestParseFileZilla(t *testing.T) {
	sites, unsupported, err := parseSiteImport([]byte(fzSample))
	if err != nil {
		t.Fatal(err)
	}
	if unsupported != 1 || len(sites) != 4 {
		t.Fatalf("got %d sites, %d unsupported", len(sites), unsupported)
	}
	byHost := map[string]importedSite{}
	for _, s := range sites {
		byHost[s.Host] = s
	}
	box := byHost["box.example.net"]
	if box.Protocol != "sftp" || box.Port != 22 || box.Username != "seed" || box.Password != "hunter2" || box.RemotePath != "/home/seed" || box.Name != "Seedbox" {
		t.Fatalf("seedbox = %+v", box)
	}
	files := byHost["ftp.example.org"]
	if files.Protocol != "ftps" || !files.Implicit || files.Port != 990 || files.Name != "Work / Files" {
		t.Fatalf("files = %+v", files)
	}
	dep := byHost["keys.example.org"]
	if dep.KeyPath != `C:\keys\id_ed25519` || dep.Port != 22 || dep.Name != "Work / Old / Deploy" {
		t.Fatalf("deploy = %+v", dep)
	}
	plain := byHost["plain.example.org"]
	if plain.Protocol != "ftp" || plain.Port != 21 || plain.Username != "anonymous" || plain.Name != "plain.example.org" {
		t.Fatalf("plain = %+v", plain)
	}
}

func TestFileZillaPasswordsWithAMasterPasswordAreNotGuessed(t *testing.T) {
	x := `<FileZilla3><Servers><Server><Host>h</Host><Protocol>1</Protocol><User>u</User>
		<Pass encoding="crypt">` + base64.StdEncoding.EncodeToString([]byte("scrambled")) + `</Pass></Server></Servers></FileZilla3>`
	sites, _, err := parseSiteImport([]byte(x))
	if err != nil || len(sites) != 1 || sites[0].Password != "" {
		t.Fatalf("%+v %v", sites, err)
	}
}

const winscpSample = `[Configuration\CDCache]
foo=bar

[Sessions\Default%20Settings]
FSProtocol=1

[Sessions\Seedbox]
HostName=box.example.net
PortNumber=2222
UserName=seed
FSProtocol=2
RemoteDirectory=/home/seed/files
PublicKeyFile=C%3A%5Ckeys%5Cid_rsa.ppk

[Sessions\Work/Ftp%20Site]
HostName=ftp.example.org
UserName=me%40corp
FSProtocol=5
Ftps=2

[Sessions\Work/Implicit]
HostName=imp.example.org
FSProtocol=5
Ftps=1

[Sessions\Bucket]
HostName=s3.amazonaws.com
FSProtocol=7

[Sessions\Work]
`

func TestParseWinSCP(t *testing.T) {
	sites, unsupported, err := parseSiteImport([]byte(winscpSample))
	if err != nil {
		t.Fatal(err)
	}
	if unsupported != 1 || len(sites) != 3 {
		t.Fatalf("got %d sites, %d unsupported: %+v", len(sites), unsupported, sites)
	}
	s := sites[0]
	if s.Name != "Seedbox" || s.Protocol != "sftp" || s.Port != 2222 || s.RemotePath != "/home/seed/files" || s.KeyPath != `C:\keys\id_rsa.ppk` {
		t.Fatalf("seedbox = %+v", s)
	}
	f := sites[1]
	if f.Name != "Work / Ftp Site" || f.Protocol != "ftps" || f.Implicit || f.Port != 21 || f.Username != "me@corp" {
		t.Fatalf("ftp = %+v", f)
	}
	if i := sites[2]; !i.Implicit || i.Port != 990 || i.Protocol != "ftps" {
		t.Fatalf("implicit = %+v", i)
	}
}

func TestImportRejectsOtherFiles(t *testing.T) {
	if _, _, err := parseSiteImport([]byte("just some text")); err == nil {
		t.Fatal("expected an error")
	}
}
