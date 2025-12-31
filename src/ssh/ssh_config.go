package ssh

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type SSHConfig struct {
	Path string
	Sections []Section
}

type Section struct {
	Name string
	Hostname     string
	Port         int
	User         string
	IdentityFile string
}

func ParseConfig(path string) (*SSHConfig, error) {

	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := SSHConfig{
		Path:    path,
		Sections: []Section{},
	}

	var current []*Section

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.ToLower(parts[0])
		val := parts[1]

		switch key {
		case "host":

			current = nil

			for _, name := range parts[1:] {

				cfg.Sections = append(cfg.Sections, Section{Name: name})

				curPtr := &cfg.Sections[len(cfg.Sections)-1]
				current = append(current, curPtr)
			}

		case "hostname":
			for _, s := range current {
				s.Hostname = val
			}

		case "port":
			if p, err := strconv.Atoi(val); err != nil {
				return nil, err
			} else {
				for _, s := range current {
					s.Port = p
				}
			}

		case "user":
			for _, s := range current {
				s.User = val
			}

		case "identityfile":
			for _, s := range current {
				s.IdentityFile = ExpandPath(val)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &cfg, nil
}
