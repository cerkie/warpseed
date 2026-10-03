package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"warpseed/internal/queue"
)

/* Importing sites from FileZilla (an exported or sitemanager.xml file) and
   WinSCP (an exported configuration .ini). Only what warpseed can use is
   taken: SFTP, FTP and FTPS sites. Anything else is counted and skipped.
   FileZilla saves passwords as base64 when asked to; WinSCP scrambles them
   with its own scheme, so those are not imported. */

type importedSite struct {
	Name       string
	Protocol   string // sftp | ftp | ftps
	Host       string
	Port       int
	Username   string
	Password   string
	KeyPath    string
	RemotePath string
	Implicit   bool
}

// ImportResult says what an import did, for the notice the user sees.
type ImportResult struct {
	Added       int `json:"added"`
	Duplicates  int `json:"duplicates"`  // already saved, left alone
	Unsupported int `json:"unsupported"` // WebDAV, S3, Storj and the like
	Passwords   int `json:"passwords"`   // of the added sites, how many came with a password
	PpkKeys     int `json:"ppkKeys"`     // PuTTY .ppk keys, which warpseed cannot read; the site is saved without one
}

// ImportSites reads a FileZilla or WinSCP export and saves its sites.
func (a *App) ImportSites(file string) (ImportResult, error) {
	var res ImportResult
	if a.store == nil {
		return res, errNoStore
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return res, err
	}
	found, unsupported, err := parseSiteImport(data)
	if err != nil {
		return res, err
	}
	res.Unsupported = unsupported
	existing, err := a.store.Sites()
	if err != nil {
		return res, err
	}
	key := func(proto, host string, port int, user string) string {
		return strings.ToLower(fmt.Sprintf("%s|%s|%d|%s", proto, host, port, user))
	}
	have := map[string]bool{}
	for _, s := range existing {
		have[key(s.Protocol, s.Host, s.Port, s.Username)] = true
	}
	for _, s := range found {
		k := key(s.Protocol, s.Host, s.Port, s.Username)
		if have[k] {
			res.Duplicates++
			continue
		}
		if strings.HasSuffix(strings.ToLower(s.KeyPath), ".ppk") {
			res.PpkKeys++
			s.KeyPath = ""
		}
		opts := siteOptions{Implicit: s.Implicit, KeyPath: s.KeyPath}
		optJSON, _ := json.Marshal(opts)
		if _, err := a.SaveSite(queue.Site{
			Name: s.Name, Protocol: s.Protocol, Host: s.Host, Port: s.Port,
			Username: s.Username, RemotePath: s.RemotePath, OptionsJSON: string(optJSON),
		}, s.Password); err != nil {
			return res, fmt.Errorf("%s: %w", s.Name, err)
		}
		have[k] = true
		res.Added++
		if s.Password != "" {
			res.Passwords++
		}
	}
	return res, nil
}

// parseSiteImport recognises the file by its contents: XML is FileZilla,
// an ini with [Sessions\...] sections is WinSCP.
func parseSiteImport(data []byte) (sites []importedSite, unsupported int, err error) {
	text := strings.TrimPrefix(string(data), string(rune(0xFEFF)))
	switch {
	case strings.HasPrefix(strings.TrimSpace(text), "<"):
		return parseFileZilla([]byte(text))
	case strings.Contains(text, `[Sessions\`):
		sites, unsupported = parseWinSCP(text)
		return sites, unsupported, nil
	}
	return nil, 0, fmt.Errorf("this is not a FileZilla (XML) or WinSCP (.ini) export")
}

// ---- FileZilla ----

type fzServer struct {
	Host      string `xml:"Host"`
	Port      int    `xml:"Port"`
	Protocol  int    `xml:"Protocol"`
	User      string `xml:"User"`
	Logontype int    `xml:"Logontype"`
	Pass      struct {
		Encoding string `xml:"encoding,attr"`
		Value    string `xml:",chardata"`
	} `xml:"Pass"`
	Keyfile   string `xml:"Keyfile"`
	Name      string `xml:"Name"`
	RemoteDir string `xml:"RemoteDir"`
}

type fzFolder struct {
	Name    string     `xml:",chardata"`
	Servers []fzServer `xml:"Server"`
	Folders []fzFolder `xml:"Folder"`
}

type fzFile struct {
	Servers []fzServer `xml:"Servers>Server"`
	Folders []fzFolder `xml:"Servers>Folder"`
}

func parseFileZilla(data []byte) ([]importedSite, int, error) {
	var f fzFile
	if err := xml.Unmarshal(data, &f); err != nil {
		return nil, 0, fmt.Errorf("not a readable FileZilla file: %w", err)
	}
	var out []importedSite
	unsupported := 0
	add := func(s fzServer, folder string) {
		site, ok := fzSite(s)
		if !ok {
			unsupported++
			return
		}
		if folder != "" {
			site.Name = folder + " / " + site.Name
		}
		out = append(out, site)
	}
	var walk func(folders []fzFolder, prefix string)
	walk = func(folders []fzFolder, prefix string) {
		for _, fo := range folders {
			name := strings.TrimSpace(fo.Name)
			if prefix != "" {
				name = prefix + " / " + name
			}
			for _, s := range fo.Servers {
				add(s, name)
			}
			walk(fo.Folders, name)
		}
	}
	for _, s := range f.Servers {
		add(s, "")
	}
	walk(f.Folders, "")
	return out, unsupported, nil
}

// fzSite converts one FileZilla server. Protocol numbers: 0 FTP (FileZilla
// tries TLS first), 1 SFTP, 3 FTPS implicit, 4 FTPES explicit, 6 plain FTP.
func fzSite(s fzServer) (importedSite, bool) {
	site := importedSite{
		Host: strings.TrimSpace(s.Host), Port: s.Port, Username: s.User,
		Name: strings.TrimSpace(s.Name), RemotePath: fzRemoteDir(s.RemoteDir),
	}
	switch s.Protocol {
	case 0, 4:
		site.Protocol = "ftps"
	case 3:
		site.Protocol, site.Implicit = "ftps", true
	case 1:
		site.Protocol = "sftp"
	case 6:
		site.Protocol = "ftp"
	default:
		return site, false
	}
	if site.Host == "" {
		return site, false
	}
	if site.Port == 0 {
		site.Port = map[string]int{"sftp": 22, "ftp": 21, "ftps": 21}[site.Protocol]
		if site.Implicit {
			site.Port = 990
		}
	}
	if site.Name == "" {
		site.Name = site.Host
	}
	if s.Logontype == 0 && site.Username == "" {
		site.Username = "anonymous"
	}
	switch s.Pass.Encoding {
	case "base64":
		if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s.Pass.Value)); err == nil {
			site.Password = string(b)
		}
	case "":
		site.Password = s.Pass.Value
	}
	if s.Logontype == 5 && site.Protocol == "sftp" {
		site.KeyPath = strings.TrimSpace(s.Keyfile)
	}
	return site, true
}

// fzRemoteDir decodes FileZilla's "1 0 4 home 4 user" form (server type, a
// prefix length, then length-prefixed folder names) into "/home/user".
func fzRemoteDir(enc string) string {
	f := strings.Fields(enc)
	if len(f) < 3 || f[0] != "1" {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(enc), f[0]))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, f[1]))
	var parts []string
	for rest != "" {
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			return ""
		}
		n, err := strconv.Atoi(rest[:sp])
		if err != nil || n < 0 || sp+1+n > len(rest) {
			return ""
		}
		parts = append(parts, rest[sp+1:sp+1+n])
		rest = strings.TrimSpace(rest[sp+1+n:])
	}
	return "/" + strings.Join(parts, "/")
}

// ---- WinSCP ----

func parseWinSCP(text string) ([]importedSite, int) {
	type raw map[string]string
	var order []string
	secs := map[string]raw{}
	var cur raw
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			name := line[1 : len(line)-1]
			cur = nil
			if rest, ok := strings.CutPrefix(name, `Sessions\`); ok {
				if n, err := url.PathUnescape(rest); err == nil {
					rest = n
				}
				if rest != "" && rest != "Default Settings" && !strings.HasPrefix(rest, "Default Settings") {
					cur = raw{}
					secs[rest] = cur
					order = append(order, rest)
				}
			}
		case cur != nil:
			if k, v, ok := strings.Cut(line, "="); ok {
				cur[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	var out []importedSite
	unsupported := 0
	for _, name := range order {
		r := secs[name]
		host := r["HostName"]
		if host == "" {
			continue // a folder entry, or a session with no host
		}
		fs, _ := strconv.Atoi(r["FSProtocol"])
		if _, set := r["FSProtocol"]; !set {
			fs = 1 // unset means SFTP
		}
		site := importedSite{
			Name: strings.ReplaceAll(name, "/", " / "), Host: host, Username: unescapeWinSCP(r["UserName"]),
			RemotePath: unescapeWinSCP(r["RemoteDirectory"]), KeyPath: unescapeWinSCP(r["PublicKeyFile"]),
		}
		site.Port, _ = strconv.Atoi(r["PortNumber"])
		ftps, _ := strconv.Atoi(r["Ftps"])
		switch fs {
		case 0, 1, 2:
			site.Protocol = "sftp"
		case 5:
			site.Protocol = "ftp"
			if ftps != 0 {
				site.Protocol, site.Implicit = "ftps", ftps == 1
			}
		default:
			unsupported++
			continue
		}
		if site.Port == 0 {
			site.Port = map[string]int{"sftp": 22, "ftp": 21, "ftps": 21}[site.Protocol]
			if site.Implicit {
				site.Port = 990
			}
		}
		if site.Protocol != "sftp" {
			site.KeyPath = ""
		}
		out = append(out, site)
	}
	return out, unsupported
}

// unescapeWinSCP undoes the %XX escapes WinSCP puts in values.
func unescapeWinSCP(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}
