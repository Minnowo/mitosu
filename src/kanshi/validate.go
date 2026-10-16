// Checks on the contents of a decoded sample against the limits in
// ../kanshi/docs/protocol.md ("sample"), so that later code (the store,
// the UI) can rely on them.

package kanshi

import (
	"fmt"
	"regexp"
)

const (
	// maxEntries caps fs, disks, net and containers (protocol: 64 each).
	maxEntries = 64
	// maxErrors caps the errors list (protocol: 32).
	maxErrors = 32
	// maxCores is kanshi's own cap on cores (docs/threat-model.md in
	// kanshi); the protocol does not state one.
	maxCores = 8192
	// maxMessage caps an error message (protocol: 512 bytes).
	maxMessage = 512
	// maxName caps container, Compose project and service names
	// (protocol: 128 bytes).
	maxName = 128
	// maxImage caps an image reference (protocol: 256 bytes).
	maxImage = 256
	// maxText caps strings the protocol gives no limit for (mount points,
	// device and interface names, version strings).
	maxText = 4096
	// maxWord caps short words (states, health, error sources).
	maxWord = 32
)

var (
	namePattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	idPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	bootIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	imagePattern  = regexp.MustCompile(`^[\x21-\x7e]+$`)
)

// checkSample checks a sample: the errors list is present, at least one
// section is, lists are within their caps, names and IDs have their
// documented form, and strings are within their caps.
func checkSample(s *Sample) error {
	if s.Errors == nil {
		return fmt.Errorf("%w: sample %d has no errors list", ErrProtocol, s.Seq)
	}
	if s.BootID == nil && s.UptimeMs == nil && s.CPU == nil && s.Mem == nil && s.Load == nil &&
		s.Vmstat == nil && s.FS == nil && s.Disks == nil && s.Net == nil && s.Containers == nil {
		return fmt.Errorf("%w: sample %d has no section", ErrProtocol, s.Seq)
	}
	if err := checkCount(s.Seq, "errors", len(*s.Errors), maxErrors); err != nil {
		return err
	}
	for _, p := range *s.Errors {
		if err := checkText("error source", p.Source, maxWord); err != nil {
			return err
		}
		if err := checkText("error message", p.Message, maxMessage); err != nil {
			return err
		}
	}
	if s.BootID != nil && !bootIDPattern.MatchString(*s.BootID) {
		return fmt.Errorf("%w: sample %d boot_id %q", ErrProtocol, s.Seq, Printable(*s.BootID))
	}
	if s.CPU != nil {
		if err := checkCount(s.Seq, "cpu cores", len(s.CPU.Cores), maxCores); err != nil {
			return err
		}
	}
	if s.FS != nil {
		if err := checkCount(s.Seq, "fs", len(*s.FS), maxEntries); err != nil {
			return err
		}
		for _, f := range *s.FS {
			for _, t := range []string{f.Mount, f.Source, f.Type} {
				if err := checkText("fs", t, maxText); err != nil {
					return err
				}
			}
		}
	}
	if s.Disks != nil {
		if err := checkCount(s.Seq, "disks", len(*s.Disks), maxEntries); err != nil {
			return err
		}
		for _, d := range *s.Disks {
			if err := checkText("disk name", d.Name, maxText); err != nil {
				return err
			}
		}
	}
	if s.Net != nil {
		if err := checkCount(s.Seq, "net", len(*s.Net), maxEntries); err != nil {
			return err
		}
		for _, n := range *s.Net {
			if err := checkText("interface name", n.Name, maxText); err != nil {
				return err
			}
		}
	}
	if s.Containers != nil {
		if err := checkCount(s.Seq, "containers", len(*s.Containers), maxEntries); err != nil {
			return err
		}
		for _, c := range *s.Containers {
			if err := checkContainer(s.Seq, c); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkContainer checks one container's identifying strings.
func checkContainer(seq uint64, c Container) error {
	if !checkName(c.Name) {
		return fmt.Errorf("%w: sample %d container name %q", ErrProtocol, seq, Printable(c.Name))
	}
	if !idPattern.MatchString(c.ID) {
		return fmt.Errorf("%w: sample %d container %s id %q", ErrProtocol, seq, c.Name, Printable(c.ID))
	}
	for _, label := range []string{c.Project, c.Service} {
		if label != "" && !checkName(label) {
			return fmt.Errorf("%w: sample %d container %s label %q", ErrProtocol, seq, c.Name, Printable(label))
		}
	}
	if len(c.Image) > maxImage || !imagePattern.MatchString(c.Image) {
		return fmt.Errorf("%w: sample %d container %s image %q", ErrProtocol, seq, c.Name, Printable(c.Image))
	}
	if err := checkText("container state", c.State, maxWord); err != nil {
		return err
	}
	return checkText("container health", c.Health, maxWord)
}

// checkName reports whether s is a valid container or Compose name.
func checkName(s string) bool {
	return len(s) <= maxName && namePattern.MatchString(s)
}

// checkCount checks one list length against its cap.
func checkCount(seq uint64, name string, n, max int) error {
	if n > max {
		return fmt.Errorf("%w: sample %d has %d %s entries, at most %d allowed", ErrProtocol, seq, n, name, max)
	}
	return nil
}

// checkText bounds a string from the server.
func checkText(what, s string, max int) error {
	if len(s) > max {
		return fmt.Errorf("%w: %s text of %d bytes, at most %d allowed", ErrProtocol, what, len(s), max)
	}
	return nil
}
