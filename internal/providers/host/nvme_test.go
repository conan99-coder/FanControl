package host

import (
	"os"
	"path/filepath"
	"testing"
)

// buildFakeSysfs creates a minimal /sys/class/nvme tree with model, serial,
// firmware, namespace size, and hwmon temp for two drives:
//
//	sys/class/nvme/nvme0/
//	  model, serial, firmware
//	  nvme0n1/
//	    hwmon0/temp1_input  (45000 = 45.0C)
//	sys/class/block/nvme0n1/size  (3907029168 sectors = 2TiB)
//
//	...and nvme1 similarly (52.0C).
func buildFakeSysfs(t *testing.T, root string) {
	t.Helper()
	mk := func(path string, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	nvmeBase := filepath.Join(root, "sys", "class", "nvme")
	blockBase := filepath.Join(root, "sys", "class", "block")

	// nvme0
	mk(filepath.Join(nvmeBase, "nvme0", "model"), "Samsung SSD 990 PRO 2TB\n")
	mk(filepath.Join(nvmeBase, "nvme0", "serial"), "S6P2NX0T712345A\n")
	mk(filepath.Join(nvmeBase, "nvme0", "firmware"), "4B2QJXD7\n")
	mk(filepath.Join(nvmeBase, "nvme0", "nvme0n1", "hwmon0", "temp1_input"), "45000\n")
	mk(filepath.Join(blockBase, "nvme0n1", "size"), "3907029168\n")

	// nvme1
	mk(filepath.Join(nvmeBase, "nvme1", "model"), "Samsung SSD 990 PRO 2TB\n")
	mk(filepath.Join(nvmeBase, "nvme1", "serial"), "S6P2NX0T776543B\n")
	mk(filepath.Join(nvmeBase, "nvme1", "firmware"), "4B2QJXD7\n")
	mk(filepath.Join(nvmeBase, "nvme1", "nvme1n1", "hwmon0", "temp1_input"), "52000\n")
	mk(filepath.Join(blockBase, "nvme1n1", "size"), "3907029168\n")
}

func TestReadDrives(t *testing.T) {
	root := t.TempDir()
	buildFakeSysfs(t, root)
	p := &Provider{Root: root}
	drives := p.readDrives()
	if len(drives) != 2 {
		t.Fatalf("expected 2 drives, got %d", len(drives))
	}

	d0 := drives[0]
	if d0.Device != "nvme0n1" {
		t.Errorf("device = %q, want nvme0n1", d0.Device)
	}
	if d0.Model != "Samsung SSD 990 PRO 2TB" {
		t.Errorf("model = %q", d0.Model)
	}
	if d0.Serial != "S6P2NX0T712345A" {
		t.Errorf("serial = %q", d0.Serial)
	}
	if d0.Firmware != "4B2QJXD7" {
		t.Errorf("firmware = %q", d0.Firmware)
	}
	// size = 3907029168 sectors * 512 = 2000398934016 bytes (~2TB)
	if d0.SizeBytes != 3907029168*512 {
		t.Errorf("size = %v, want %v", d0.SizeBytes, 3907029168*512)
	}
	if d0.Temp != 45.0 {
		t.Errorf("temp = %v, want 45.0", d0.Temp)
	}

	d1 := drives[1]
	if d1.Device != "nvme1n1" {
		t.Errorf("device = %q, want nvme1n1", d1.Device)
	}
	if d1.Serial != "S6P2NX0T776543B" {
		t.Errorf("serial = %q", d1.Serial)
	}
	if d1.Temp != 52.0 {
		t.Errorf("temp = %v, want 52.0", d1.Temp)
	}
}

func TestBlockSize(t *testing.T) {
	root := t.TempDir()
	buildFakeSysfs(t, root)
	p := &Provider{Root: root}
	if got := p.blockSize("nvme0n1"); got != 3907029168*512 {
		t.Errorf("blockSize = %v, want %v", got, 3907029168*512)
	}
	// Missing device -> 0
	if got := p.blockSize("nvmeX9"); got != 0 {
		t.Errorf("missing blockSize = %v, want 0", got)
	}
}
