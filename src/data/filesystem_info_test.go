// Tests for the `df -B1 -P` parser behind `mitosu stat fs`.

package data

import (
	"testing"

	"mitosu/src/shell"
)

const dfOutput = `Filesystem      1-blocks        Used   Available Capacity Mounted on
/dev/sda1    79048540160  8748609536 67036561408      12% /
tmpfs          812761088           0   812761088       0% /dev/shm
/dev/sda15     129718272     9322496   120395776       8% /boot/efi
`

func TestFilesystemParsesDf(t *testing.T) {
	var stat FSSystemStat
	stat.ParseCmdOutput(shell.PosixShellType, []string{dfOutput})
	if len(stat.FSInfos) != 3 {
		t.Fatalf("got %d filesystems, want 3: %+v", len(stat.FSInfos), stat.FSInfos)
	}
	root := stat.FSInfos[0]
	if root.Filesystem != "/dev/sda1" || root.MountPoint != "/" || root.Used != 8748609536 || root.Free != 67036561408 {
		t.Fatalf("first filesystem = %+v", root)
	}
}

func TestFilesystemShortLineDoesNotPanic(t *testing.T) {
	var stat FSSystemStat
	header := "Filesystem      1-blocks        Used   Available Capacity Mounted on\n"
	stat.ParseCmdOutput(shell.PosixShellType, []string{header + "/dev/x 1\n"})
	if len(stat.FSInfos) != 0 {
		t.Fatalf("a cut-off line was parsed: %+v", stat.FSInfos)
	}
}
