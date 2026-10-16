// Tests for reading kanshi responses: kanshi's golden files
// (testdata/kanshi-golden, protocol 1.0) and hostile or broken output.

package kanshi

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	goldenDir = "../../testdata/kanshi-golden"
	// testStore replaces the golden files' normalized store ID.
	testStore = "0123456789abcdef0123456789abcdef"
)

func TestGoldenVersion(t *testing.T) {
	v, err := ReadVersion(bytes.NewReader(golden(t, "version.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kanshi != "1.0.0" || v.Format != "1.0" {
		t.Fatalf("got %+v", v)
	}
}

func TestGoldenExportHostBasic(t *testing.T) {
	samples, header, end, err := readAll(golden(t, "export-host-basic.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if header.Store != testStore || header.Format != "1.0" || len(samples) != 2 {
		t.Fatalf("header %+v, %d samples", header, len(samples))
	}
	if end != (End{Next: 2}) {
		t.Fatalf("end %+v", end)
	}
	s := samples[0]
	if s.Seq != 1 || s.BootID == nil || s.CPU == nil || len(s.CPU.Cores) == 0 || s.Mem == nil || s.Load == nil ||
		s.Vmstat == nil || s.FS == nil || s.Disks == nil || s.Net == nil || s.Containers == nil || s.Errors == nil {
		t.Fatalf("sections missing from the full sample: %+v", s)
	}
	running := 0
	for _, c := range *s.Containers {
		if c.State == "running" {
			running++
			if c.Stats == nil {
				t.Fatalf("running container %s without stats", c.Name)
			}
		} else if c.Stats != nil {
			t.Fatalf("%s container %s with stats", c.State, c.Name)
		}
	}
	if running == 0 || running == len(*s.Containers) {
		t.Fatalf("want containers in several states, got %d of %d running", running, len(*s.Containers))
	}
	if len(*s.Errors) == 0 || (*s.Errors)[0].Source != "fs" {
		t.Fatalf("errors %+v, want the fs error", *s.Errors)
	}
}

func TestGoldenExportHostOnlyHasNoContainers(t *testing.T) {
	samples, _, _, err := readAll(golden(t, "export-host-only.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Containers != nil {
		t.Fatalf("got %d samples, containers %v", len(samples), samples[0].Containers)
	}
}

func TestGoldenLimitAndNothingNew(t *testing.T) {
	samples, _, end, err := readAllLimit(golden(t, "export-limit.jsonl"), 0, 1)
	if err != nil || len(samples) != 1 || !end.More || end.Next != 1 {
		t.Fatalf("limit: %d samples, end %+v, err %v", len(samples), end, err)
	}
	samples, _, end, err = readAll(golden(t, "export-nothing-new.jsonl"), 1)
	if err != nil || len(samples) != 0 || end != (End{Next: 1}) {
		t.Fatalf("nothing new: %d samples, end %+v, err %v", len(samples), end, err)
	}
}

func TestGoldenErrors(t *testing.T) {
	for file, code := range map[string]string{"error-no-store.jsonl": "no_store", "error-bad-request.jsonl": "bad_request"} {
		_, _, _, err := readAll(golden(t, file), 0)
		var se *ServerError
		if !errors.As(err, &se) || se.Code != code {
			t.Errorf("%s: err %v, want code %s", file, err, code)
		}
	}
}

func TestGzipResponse(t *testing.T) {
	samples, _, end, err := readAll(gzipped(t, golden(t, "export-host-basic.jsonl")), 0)
	if err != nil || len(samples) != 2 || end.Next != 2 {
		t.Fatalf("%d samples, end %+v, err %v", len(samples), end, err)
	}
}

func TestOutputBeforeKanshiIsRefused(t *testing.T) {
	noise := []byte("Welcome to web1!\n")
	resp := golden(t, "export-host-basic.jsonl")
	for name, data := range map[string][]byte{
		"plain":   append(append([]byte{}, noise...), resp...),
		"gzip":    append(append([]byte{}, noise...), gzipped(t, resp)...),
		"json":    []byte(`{"motd":"hi"}` + "\n"),
		"version": golden(t, "version.jsonl"),
	} {
		if _, _, _, err := readAll(data, 0); !errors.Is(err, ErrNotKanshiOutput) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

func TestCutOffResponsesAreTruncated(t *testing.T) {
	resp := golden(t, "export-host-basic.jsonl")
	withoutEnd := resp[:bytes.LastIndex(resp[:len(resp)-1], []byte("\n"))+1]
	z := gzipped(t, resp)
	for name, data := range map[string][]byte{
		"no end line": withoutEnd,
		"mid line":    resp[:len(resp)-10],
		"header only": resp[:bytes.IndexByte(resp, '\n')+1],
		"gzip cut":    z[:len(z)/2],
		"gzip no end": gzipped(t, withoutEnd),
		"version cut": []byte(`{"type":"version","kanshi":"1.0.0","format":"1.0"}`),
	} {
		if _, _, _, err := readAll(data, 0); !errors.Is(err, ErrTruncated) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

func TestFormatVersions(t *testing.T) {
	resp := string(golden(t, "export-host-basic.jsonl"))
	newer := strings.Replace(resp, `"format":"1.0"`, `"format":"1.7","new_field":[1,2]`, 1)
	newer = strings.Replace(newer, `"errors":[`, `"gpu":{"util":3},"errors":[`, 1)
	if !strings.Contains(newer, `"gpu"`) || !strings.Contains(newer, `"new_field"`) {
		t.Fatal("replacement did not apply")
	}
	if _, _, _, err := readAll([]byte(newer), 0); err != nil {
		t.Fatalf("a newer minor with unknown fields was refused: %v", err)
	}
	for _, f := range []string{"2.0", "0.6"} {
		data := strings.Replace(resp, `"format":"1.0"`, `"format":"`+f+`"`, 1)
		if _, _, _, err := readAll([]byte(data), 0); !errors.Is(err, ErrUnsupportedFormat) {
			t.Errorf("format %s: err %v", f, err)
		}
	}
	for _, f := range []string{"1", "1.x", "01.0"} {
		data := strings.Replace(resp, `"format":"1.0"`, `"format":"`+f+`"`, 1)
		if _, _, _, err := readAll([]byte(data), 0); !errors.Is(err, ErrProtocol) {
			t.Errorf("format %s: err %v", f, err)
		}
	}
}

func TestProtocolViolations(t *testing.T) {
	resp := string(golden(t, "export-host-basic.jsonl"))
	cases := map[string]string{
		"seq not increasing":   strings.Replace(resp, `"seq":2,`, `"seq":1,`, 1),
		"bad store":            strings.Replace(resp, testStore, "STORE", 1),
		"wrong next":           strings.Replace(resp, `"next":2`, `"next":3`, 1),
		"line after end":       resp + `{"type":"end","next":2,"more":false,"lost_through":0}` + "\n",
		"unknown line type":    strings.Replace(resp, `{"type":"sample","seq":2,`, `{"type":"note","seq":2,`, 1),
		"no errors list":       strings.Replace(resp, `"errors":[`, `"x":[`, 1),
		"negative seq":         strings.Replace(resp, `"seq":2,`, `"seq":-2,`, 1),
		"negative counter":     strings.Replace(resp, `"total":{"user":`, `"total":{"user":-`, 1),
		"upper-case type":      strings.Replace(resp, `{"type":"sample","seq":2,`, `{"TYPE":"sample","seq":2,`, 1),
		"repeated type":        strings.Replace(resp, `{"type":"sample","seq":2,`, `{"type":"sample","type":"header","seq":2,`, 1),
		"case-variant key":     strings.Replace(resp, `"seq":2,`, `"seq":2,"Seq":7,`, 1),
		"null section":         strings.Replace(resp, `"vmstat":{`, `"cpu2":null,"vmstat":{`, 1),
		"missing time_ms":      strings.Replace(resp, `"seq":2,"time_ms":0,`, `"seq":2,`, 1),
		"missing counter":      strings.Replace(resp, `"total":{"user":`, `"total":{"usr":`, 1),
		"missing more":         strings.Replace(resp, `"more":false,`, ``, 1),
		"crlf":                 strings.Replace(resp, "}\n{\"type\":\"end\"", "}\r\n{\"type\":\"end\"", 1),
		"raw C1 control":       strings.Replace(resp, `"errors":[`, `"note":"\u00c2\u009b","x":"`+"\xc2\x9b"+`","errors":[`, 1),
		"invalid utf-8":        strings.Replace(resp, `"errors":[`, `"x":"`+"\xff"+`","errors":[`, 1),
		"byte after end":       resp + " ",
		"bad container name":   strings.Replace(resp, `"name":"blog-db-1"`, `"name":"-blog"`, 1),
		"short container id":   strings.Replace(resp, `"id":"`, `"id":"ab","x":"`, 1),
		"lost_through only":    strings.Replace(resp, `"lost_through":0`, `"lost_through":"x"`, 1),
		"lost_through > int64": strings.Replace(resp, `"lost_through":0`, `"lost_through":9223372036854775808`, 1),
		"time_ms > int64":      strings.Replace(resp, `"seq":2,"time_ms":0,`, `"seq":2,"time_ms":9223372036854775808,`, 1),
		"error after header":   strings.Replace(resp, `{"type":"sample","seq":2,`, `{"type":"error","code":"internal","message":"x","seq":2,`, 1),
	}
	for name, data := range cases {
		if data == resp {
			t.Fatalf("%s: replacement did not apply", name)
		}
		if _, _, _, err := readAll([]byte(data), 0); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: err %v", name, err)
		}
	}
	if _, _, _, err := readAll([]byte(resp), 1); !errors.Is(err, ErrProtocol) {
		t.Errorf("sample at or below --since: err %v", err)
	}
}

func TestOversizedInput(t *testing.T) {
	resp := string(golden(t, "export-host-only.jsonl"))
	long := strings.Replace(resp, `"errors":[`, `"pad":"`+strings.Repeat("x", MaxLineBytes)+`","errors":[`, 1)
	if long == resp {
		t.Fatal("replacement did not apply")
	}
	if _, _, _, err := readAll([]byte(long), 0); !errors.Is(err, ErrProtocol) {
		t.Errorf("long line: err %v", err)
	}
	var many []string
	for i := 0; i <= maxEntries; i++ {
		many = append(many, `{"name":"c`+strings.Repeat("x", i%3)+`","id":"","image":"","state":"exited"}`)
	}
	data := strings.Replace(resp, `"errors":[`, `"containers":[`+strings.Join(many, ",")+`],"errors":[`, 1)
	if _, _, _, err := readAll([]byte(data), 0); !errors.Is(err, ErrProtocol) {
		t.Errorf("65 containers: err %v", err)
	}
	data = strings.Replace(resp, `"errors":[`, `"errors":[{"source":"fs","message":"`+strings.Repeat("m", maxText+1)+`"},`, 1)
	if _, _, _, err := readAll([]byte(data), 0); !errors.Is(err, ErrProtocol) {
		t.Errorf("long message: err %v", err)
	}
}

func TestOnSampleErrorStops(t *testing.T) {
	stop := errors.New("stop")
	calls := 0
	_, _, err := ReadExport(bytes.NewReader(golden(t, "export-host-basic.jsonl")), 0, 0, func(Sample, []byte) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("err %v after %d calls", err, calls)
	}
}

func TestPrintable(t *testing.T) {
	if got := Printable("a\x1b[2Jb\n\xff"); got != "a?[2Jb??" {
		t.Fatalf("got %q", got)
	}
	if got := Printable(strings.Repeat("x", 300)); len(got) != 200 {
		t.Fatalf("length %d", len(got))
	}
}

func TestExportCommand(t *testing.T) {
	cmd, err := ExportCommand(5, 100, true)
	if err != nil || cmd != "kanshi export --since 5 --limit 100 --gzip" {
		t.Fatalf("%q %v", cmd, err)
	}
	cmd, err = ExportCommand(0, 0, false)
	if err != nil || cmd != "kanshi export --since 0" {
		t.Fatalf("%q %v", cmd, err)
	}
	if _, err := ExportCommand(0, MaxLimit+1, false); err == nil {
		t.Fatal("limit above the maximum accepted")
	}
}

func FuzzReadExport(f *testing.F) {
	files, err := filepath.Glob(filepath.Join(goldenDir, "*.jsonl"))
	if err != nil {
		f.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		data = bytes.ReplaceAll(data, []byte(`"STORE"`), []byte(`"`+testStore+`"`))
		f.Add(data)
		var z bytes.Buffer
		w := gzip.NewWriter(&z)
		if _, err := w.Write(data); err != nil {
			f.Fatal(err)
		}
		if err := w.Close(); err != nil {
			f.Fatal(err)
		}
		f.Add(z.Bytes())
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _, _ = readAll(data, 0)
		_, _ = ReadVersion(bytes.NewReader(data))
	})
}

func TestNoSectionAndErrorLineChecks(t *testing.T) {
	header := `{"type":"header","kanshi":"1.0.0","format":"1.0","store":"` + testStore + `"}` + "\n"
	empty := header + `{"type":"sample","seq":1,"time_ms":5,"errors":[]}` + "\n" + `{"type":"end","next":1,"more":false,"lost_through":0}` + "\n"
	if _, _, _, err := readAll([]byte(empty), 0); !errors.Is(err, ErrProtocol) {
		t.Errorf("sample without sections: err %v", err)
	}
	errLine := `{"type":"error","code":"internal","message":"x"}` + "\n"
	if _, _, _, err := readAll([]byte(errLine+"junk\n"), 0); !errors.Is(err, ErrProtocol) {
		t.Errorf("error line with garbage after it: err %v", err)
	}
	if _, _, _, err := readAll([]byte(`{"type":"error","message":"x"}`+"\n"), 0); !errors.Is(err, ErrProtocol) {
		t.Errorf("error line without code: err %v", err)
	}
	if _, _, _, err := readAll(nil, 0); !errors.Is(err, ErrNoOutput) {
		t.Errorf("no output: err %v", err)
	}
}

func TestLimitIsEnforced(t *testing.T) {
	resp := golden(t, "export-host-basic.jsonl")
	if _, _, _, err := readAllLimit(resp, 0, 1); !errors.Is(err, ErrProtocol) {
		t.Errorf("2 samples with --limit 1: err %v", err)
	}
	more := bytes.Replace(resp, []byte(`"more":false`), []byte(`"more":true`), 1)
	if _, _, _, err := readAllLimit(more, 0, 5); !errors.Is(err, ErrProtocol) {
		t.Errorf("more with fewer samples than the limit: err %v", err)
	}
	if _, _, end, err := readAllLimit(more, 0, 2); err != nil || !end.More {
		t.Errorf("more at the limit: end %+v err %v", end, err)
	}
}

func TestLostThrough(t *testing.T) {
	resp := golden(t, "export-host-basic.jsonl")
	lost := bytes.Replace(resp, []byte(`"lost_through":0`), []byte(`"lost_through":7`), 1)
	lost = bytes.Replace(lost, []byte(`"seq":1,`), []byte(`"seq":8,`), 1)
	lost = bytes.Replace(lost, []byte(`"seq":2,`), []byte(`"seq":9,`), 1)
	lost = bytes.Replace(lost, []byte(`"next":2`), []byte(`"next":9`), 1)
	samples, _, end, err := readAll(lost, 3)
	if err != nil || len(samples) != 2 || end.LostThrough != 7 || end.Next != 9 {
		t.Fatalf("%d samples, end %+v, err %v", len(samples), end, err)
	}
}

func TestGzipTrailingDataIsRefused(t *testing.T) {
	data := append(gzipped(t, golden(t, "export-host-basic.jsonl")), "hi\n"...)
	if _, _, _, err := readAll(data, 0); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err %v", err)
	}
}

// golden returns a golden file with the normalized store ID replaced by
// a valid one.
func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.ReplaceAll(data, []byte(`"STORE"`), []byte(`"`+testStore+`"`))
}

func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func readAll(data []byte, since uint64) ([]Sample, Header, End, error) {
	return readAllLimit(data, since, 0)
}

func readAllLimit(data []byte, since, limit uint64) ([]Sample, Header, End, error) {
	var samples []Sample
	header, end, err := ReadExport(bytes.NewReader(data), since, limit, func(s Sample, _ []byte) error {
		samples = append(samples, s)
		return nil
	})
	return samples, header, end, err
}
