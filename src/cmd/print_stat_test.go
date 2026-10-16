// Tests that the terminal output survives odd values from a server.

package cmd

import (
	"testing"

	"mitosu/src/data"
	cf "mitosu/src/display"
)

func TestPrintStatEmptyFilesystemName(t *testing.T) {
	var term cf.VirtualTerm
	PrintStat(&term, &data.FSSystemStat{FSInfos: []data.FSInfo{{Type: data.FS_Other, MountPoint: "/x"}}})
	if len(term.Lines) == 0 {
		t.Fatal("nothing printed")
	}
}

func TestPrintStatShortContainerID(t *testing.T) {
	var term cf.VirtualTerm
	PrintStat(&term, &data.DockerSystemStat{DockerContainers: []data.DockerContainer{{ID: "abc", Name: "web"}}})
	if len(term.Lines) == 0 {
		t.Fatal("nothing printed")
	}
}
