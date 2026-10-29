package scanner

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"github.com/ridoyf/unhoard/internal/tree"
)

// Helpers that build synthetic MFT records. Nothing here touches a real volume.

const tRecSize = 1024

var le = binary.LittleEndian

func utf16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = le.AppendUint16(b, u)
	}
	return b
}

func align8(n int) int { return (n + 7) &^ 7 }

func ref(rec uint64, seq uint16) uint64 { return rec | uint64(seq)<<48 }

func resAttr(typ uint32, name string, value []byte) []byte {
	nm := utf16le(name)
	voff := align8(0x18 + len(nm))
	n := align8(voff + len(value))
	a := make([]byte, n)
	le.PutUint32(a[0:], typ)
	le.PutUint32(a[4:], uint32(n))
	a[9] = byte(len(nm) / 2)
	le.PutUint16(a[0x0A:], 0x18)
	le.PutUint32(a[0x10:], uint32(len(value)))
	le.PutUint16(a[0x14:], uint16(voff))
	copy(a[0x18:], nm)
	copy(a[voff:], value)
	return a
}

type nonRes struct {
	name                                  string
	flags                                 uint16
	startVCN, lastVCN, alloc, size, total uint64
	runs                                  []byte
}

func nonResAttr(typ uint32, nr nonRes) []byte {
	hdr := 0x40
	if nr.flags&(dataCompressed|dataSparse) != 0 {
		hdr = 0x48
	}
	nm := utf16le(nr.name)
	mp := align8(hdr + len(nm))
	n := align8(mp + len(nr.runs) + 1)
	a := make([]byte, n)
	le.PutUint32(a[0:], typ)
	le.PutUint32(a[4:], uint32(n))
	a[8] = 1
	a[9] = byte(len(nm) / 2)
	le.PutUint16(a[0x0A:], uint16(hdr))
	le.PutUint16(a[0x0C:], nr.flags)
	le.PutUint64(a[0x10:], nr.startVCN)
	le.PutUint64(a[0x18:], nr.lastVCN)
	le.PutUint16(a[0x20:], uint16(mp))
	le.PutUint64(a[0x28:], nr.alloc)
	le.PutUint64(a[0x30:], nr.size)
	le.PutUint64(a[0x38:], nr.size)
	if hdr == 0x48 {
		le.PutUint64(a[0x40:], nr.total)
	}
	copy(a[hdr:], nm)
	copy(a[mp:], nr.runs)
	return a
}

func stdInfo(attrs uint32, ft int64) []byte {
	v := make([]byte, 0x48)
	le.PutUint64(v[0x08:], uint64(ft))
	le.PutUint32(v[0x20:], attrs)
	return resAttr(attrStdInfo, "", v)
}

func fileNameAttr(parent uint64, ns byte, name string) []byte {
	nm := utf16le(name)
	v := make([]byte, 0x42+len(nm))
	le.PutUint64(v, parent)
	v[0x40] = byte(len(nm) / 2)
	v[0x41] = ns
	copy(v[0x42:], nm)
	return resAttr(attrFileName, "", v)
}

func residentData(n int) []byte { return resAttr(attrData, "", make([]byte, n)) }

func reparseAttr(tag uint32) []byte { return resAttr(attrReparse, "", le.AppendUint32(nil, tag)) }

// fileRecord lays out a 1 KiB FILE record and applies the update sequence as NTFS
// does on write: the real last two bytes of each 512-byte stride move into the
// array and the check value takes their place.
func fileRecord(flags, seq uint16, base uint64, attrs ...[]byte) []byte {
	rec := make([]byte, tRecSize)
	copy(rec, "FILE")
	le.PutUint16(rec[4:], 0x30)
	le.PutUint16(rec[6:], tRecSize/512+1)
	le.PutUint16(rec[0x10:], seq)
	le.PutUint16(rec[0x14:], 0x38)
	le.PutUint16(rec[0x16:], flags)
	le.PutUint64(rec[0x20:], base)
	off := 0x38
	for _, a := range attrs {
		off += copy(rec[off:], a)
	}
	le.PutUint32(rec[off:], attrEnd)
	off += 8
	le.PutUint32(rec[0x18:], uint32(off))
	le.PutUint32(rec[0x1C:], tRecSize)
	const check = 0x0A0B
	le.PutUint16(rec[0x30:], check)
	for i := 1; i <= tRecSize/512; i++ {
		end := i*512 - 2
		copy(rec[0x30+2*i:], rec[end:end+2])
		le.PutUint16(rec[end:], check)
	}
	return rec
}

const (
	inUse    = recFlagInUse
	inUseDir = recFlagInUse | recFlagDir
)

func TestApplyFixup(t *testing.T) {
	good := func() []byte {
		rec := make([]byte, tRecSize)
		copy(rec, "FILE")
		le.PutUint16(rec[4:], 0x30)
		le.PutUint16(rec[6:], 3)
		le.PutUint16(rec[0x30:], 0x0007) // check value
		le.PutUint16(rec[0x32:], 0xAABB) // real bytes of stride 1
		le.PutUint16(rec[0x34:], 0xCCDD) // real bytes of stride 2
		le.PutUint16(rec[510:], 0x0007)
		le.PutUint16(rec[1022:], 0x0007)
		return rec
	}
	tests := []struct {
		name    string
		mutate  func([]byte) []byte
		wantErr bool
	}{
		{"valid", func(r []byte) []byte { return r }, false},
		{"stride 1 mismatch", func(r []byte) []byte { r[510] = 9; return r }, true},
		{"stride 2 mismatch", func(r []byte) []byte { r[1023] = 9; return r }, true},
		{"count too small", func(r []byte) []byte { le.PutUint16(r[6:], 1); return r }, true},
		{"count does not fit size", func(r []byte) []byte { le.PutUint16(r[6:], 4); return r }, true},
		{"array out of bounds", func(r []byte) []byte { le.PutUint16(r[4:], 1020); return r }, true},
		{"truncated", func(r []byte) []byte { return r[:600] }, true},
		{"too short", func(r []byte) []byte { return r[:8] }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := tt.mutate(good())
			err := applyFixup(rec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (le.Uint16(rec[510:]) != 0xAABB || le.Uint16(rec[1022:]) != 0xCCDD) {
				t.Errorf("stride ends not restored: %#x %#x", le.Uint16(rec[510:]), le.Uint16(rec[1022:]))
			}
		})
	}
}

func TestDecodeRuns(t *testing.T) {
	tests := []struct {
		name    string
		in      []byte
		want    []run
		wantErr bool
	}{
		{"empty", nil, nil, false},
		{"terminator only", []byte{0}, nil, false},
		{"single", []byte{0x11, 0x30, 0x20, 0}, []run{{lcn: 0x20, length: 0x30}}, false},
		{"multi with negative delta", []byte{0x31, 0x10, 0x00, 0x10, 0x00, 0x21, 0x08, 0x00, 0xF8, 0},
			[]run{{lcn: 0x1000, length: 0x10}, {lcn: 0x800, length: 8}}, false},
		{"sparse keeps previous LCN", []byte{0x11, 0x10, 0x64, 0x01, 0x10, 0x11, 0x04, 0x05, 0},
			[]run{{lcn: 100, length: 16}, {length: 16, sparse: true}, {lcn: 105, length: 4}}, false},
		{"three-byte positive delta with high bit", []byte{0x31, 0x01, 0x00, 0x80, 0x00, 0},
			[]run{{lcn: 0x8000, length: 1}}, false},
		{"no terminator at end of buffer", []byte{0x11, 0x01, 0x02}, []run{{lcn: 2, length: 1}}, false},
		{"truncated", []byte{0x31, 0x10, 0x00}, nil, true},
		{"zero length", []byte{0x11, 0x00, 0x05, 0}, nil, true},
		{"length field size zero", []byte{0x10, 0x05, 0}, nil, true},
		{"field size over 8", []byte{0x19, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0}, nil, true},
		{"negative LCN", []byte{0x11, 0x01, 0xFF, 0}, nil, true},
		{"length beyond 2^32 clusters", []byte{0x15, 0, 0, 0, 0, 1, 0}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeRuns(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseBoot(t *testing.T) {
	boot := func(sector uint16, spc, cpr byte, mft uint64) []byte {
		b := make([]byte, 512)
		copy(b[3:], "NTFS    ")
		le.PutUint16(b[0x0B:], sector)
		b[0x0D] = spc
		le.PutUint64(b[0x30:], mft)
		b[0x40] = cpr
		return b
	}
	tests := []struct {
		name    string
		in      []byte
		want    geometry
		wantErr bool
	}{
		{"typical 4K clusters, 1K records", boot(512, 8, 0xF6, 786432), geometry{512, 4096, 1024, 786432}, false},
		{"4K sectors, 4K records", boot(4096, 1, 0xF4, 10), geometry{4096, 4096, 4096, 10}, false},
		{"2 MiB clusters stored as negative power", boot(512, 0xF4, 0xF6, 3), geometry{512, 2 << 20, 1024, 3}, false},
		{"positive clusters per record", boot(512, 1, 2, 3), geometry{512, 512, 1024, 3}, false},
		{"not NTFS", append([]byte("\xEB\x52\x90FAT32   "), make([]byte, 500)...), geometry{}, true},
		{"too short", []byte("\xEB\x52\x90NTFS    "), geometry{}, true},
		{"bad sector size", boot(100, 8, 0xF6, 3), geometry{}, true},
		{"zero sectors per cluster", boot(512, 0, 0xF6, 3), geometry{}, true},
		{"absurd record size", boot(512, 8, 0x80, 3), geometry{}, true},
		{"zero MFT cluster", boot(512, 8, 0xF6, 0), geometry{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBoot(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseRecord(t *testing.T) {
	const ft = 133_000_000_000_000_000
	mtime := filetimeToUnix(ft)
	tests := []struct {
		name    string
		rec     []byte
		want    recordInfo
		names   []string // "parent:name"
		wantErr bool
	}{
		{
			name: "DOS name ignored, Win32 kept",
			rec: fileRecord(inUse, 3, 0, stdInfo(windows.FILE_ATTRIBUTE_HIDDEN, ft),
				fileNameAttr(ref(5, 5), 1, "Long File Name.txt"),
				fileNameAttr(ref(5, 5), nsDOS, "LONGFI~1.TXT"),
				residentData(100)),
			want: recordInfo{inUse: true, seq: 3, hasStdInfo: true, attrs: windows.FILE_ATTRIBUTE_HIDDEN, mtime: mtime,
				hasData: true, size: 100},
			names: []string{"5:Long File Name.txt"},
		},
		{
			name:  "Win32&DOS and POSIX names are both kept as links",
			rec:   fileRecord(inUse, 1, 0, fileNameAttr(ref(5, 5), 3, "a.txt"), fileNameAttr(ref(40, 2), 0, "b.txt")),
			want:  recordInfo{inUse: true, seq: 1},
			names: []string{"5:a.txt", "40:b.txt"},
		},
		{
			name:  "non-BMP name",
			rec:   fileRecord(inUse, 1, 0, fileNameAttr(ref(5, 5), 1, "файл😀")),
			want:  recordInfo{inUse: true, seq: 1},
			names: []string{"5:файл😀"},
		},
		{
			name: "non-resident data",
			rec:  fileRecord(inUse, 1, 0, nonResAttr(attrData, nonRes{lastVCN: 2, alloc: 12288, size: 10000, runs: []byte{0x11, 3, 9}})),
			want: recordInfo{inUse: true, seq: 1, hasData: true, size: 10000, alloc: 12288},
		},
		{
			name: "compressed data uses total allocated",
			rec: fileRecord(inUse, 1, 0, nonResAttr(attrData, nonRes{flags: dataCompressed, lastVCN: 15,
				alloc: 65536, size: 60000, total: 16384, runs: []byte{0x11, 4, 9, 0x01, 12}})),
			want: recordInfo{inUse: true, seq: 1, hasData: true, size: 60000, alloc: 16384},
		},
		{
			name: "sparse data uses total allocated",
			rec: fileRecord(inUse, 1, 0, nonResAttr(attrData, nonRes{flags: dataSparse, lastVCN: 255,
				alloc: 1 << 20, size: 1 << 20, total: 4096, runs: []byte{0x11, 1, 9, 0x01, 0xFF}})),
			want: recordInfo{inUse: true, seq: 1, hasData: true, size: 1 << 20, alloc: 4096},
		},
		{
			name: "named stream and later runlist piece ignored",
			rec: fileRecord(inUse, 1, 0,
				nonResAttr(attrData, nonRes{name: "Zone.Identifier", lastVCN: 0, alloc: 4096, size: 26, runs: []byte{0x11, 1, 9}}),
				nonResAttr(attrData, nonRes{startVCN: 100, lastVCN: 199, alloc: 999, size: 999, runs: []byte{0x11, 100, 9}})),
			want: recordInfo{inUse: true, seq: 1},
		},
		{
			name: "extension record",
			rec:  fileRecord(inUse, 4, ref(20, 7), nonResAttr(attrData, nonRes{lastVCN: 1, alloc: 8192, size: 5000, runs: []byte{0x11, 2, 9}})),
			want: recordInfo{inUse: true, seq: 4, hasBase: true, base: ref(20, 7), hasData: true, size: 5000, alloc: 8192},
		},
		{
			name: "extension of $MFT has record number 0 but a non-zero reference",
			rec:  fileRecord(inUse, 2, ref(0, 1)),
			want: recordInfo{inUse: true, seq: 2, hasBase: true, base: ref(0, 1)},
		},
		{
			name: "junction",
			rec:  fileRecord(inUseDir, 1, 0, stdInfo(windows.FILE_ATTRIBUTE_REPARSE_POINT, 0), reparseAttr(windows.IO_REPARSE_TAG_MOUNT_POINT)),
			want: recordInfo{inUse: true, isDir: true, seq: 1, hasStdInfo: true, attrs: windows.FILE_ATTRIBUTE_REPARSE_POINT,
				reparseTag: windows.IO_REPARSE_TAG_MOUNT_POINT},
		},
		{name: "free record", rec: fileRecord(0, 1, 0, fileNameAttr(ref(5, 5), 1, "gone")), want: recordInfo{}},
		{name: "never used", rec: make([]byte, tRecSize), want: recordInfo{}},
		{name: "bad signature", rec: append([]byte("BAAD"), make([]byte, tRecSize-4)...), wantErr: true},
		{name: "torn write", rec: func() []byte { r := fileRecord(inUse, 1, 0); r[1023]++; return r }(), wantErr: true},
		{name: "short $STANDARD_INFORMATION", rec: fileRecord(inUse, 1, 0, resAttr(attrStdInfo, "", make([]byte, 8))), wantErr: true},
		{name: "empty file name", rec: fileRecord(inUse, 1, 0, fileNameAttr(ref(5, 5), 1, "")), wantErr: true},
		{name: "compressed without total allocated", rec: fileRecord(inUse, 1, 0, func() []byte {
			a := nonResAttr(attrData, nonRes{lastVCN: 0, alloc: 4096, size: 1, runs: []byte{0x11, 1, 9}})
			le.PutUint16(a[0x0C:], dataCompressed)
			return a
		}()), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, names, err := parseRecord(tt.rec, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if info != tt.want {
				t.Errorf("info = %+v\nwant   %+v", info, tt.want)
			}
			var got []string
			for _, n := range names {
				got = append(got, fmt.Sprintf("%d:%s", n.parent&refMask, appendUTF16(nil, n.name)))
			}
			if !slices.Equal(got, tt.names) {
				t.Errorf("names = %q, want %q", got, tt.names)
			}
		})
	}
}

func TestMFTDataRuns(t *testing.T) {
	mft := func(lastVCN uint64, runs []byte) []byte {
		return fileRecord(inUse, 1, 0, fileNameAttr(ref(5, 5), 3, "$MFT"),
			nonResAttr(attrData, nonRes{lastVCN: lastVCN, alloc: (lastVCN + 1) * 4096, size: (lastVCN + 1) * 4096, runs: runs}))
	}
	runs, size, err := mftDataRuns(mft(15, []byte{0x21, 8, 0x00, 0x01, 0x11, 8, 0xCE, 0}), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if want := []run{{lcn: 256, length: 8}, {lcn: 206, length: 8}}; !slices.Equal(runs, want) || size != 16*4096 {
		t.Errorf("got %+v size %d, want %+v size %d", runs, size, want, 16*4096)
	}
	if _, _, err := mftDataRuns(mft(31, []byte{0x21, 16, 0x00, 0x01, 0}), 4096); err == nil {
		t.Error("runlist shorter than lastVCN should be rejected (continues in an extension record)")
	}
	if _, _, err := mftDataRuns(fileRecord(inUse, 1, 0, residentData(10)), 4096); err == nil {
		t.Error("record 0 without non-resident $DATA should be rejected")
	}
}

func TestCheckTotals(t *testing.T) {
	tests := []struct {
		alloc, used uint64
		wantErr     bool
	}{
		{100, 100, false},
		{110, 100, false},
		{111, 100, true},
		{50, 100, false},
		{49, 100, true},
		{0, 100, true},
	}
	for _, tt := range tests {
		if err := checkTotals(tt.alloc, tt.used); (err != nil) != tt.wantErr {
			t.Errorf("checkTotals(%d, %d) = %v, wantErr %v", tt.alloc, tt.used, err, tt.wantErr)
		}
	}
}

type numbered struct {
	no  uint32
	rec []byte
}

func collect(t *testing.T, nrec int, recs []numbered) *mftCollector {
	t.Helper()
	m := newCollector(nrec)
	for _, r := range recs {
		info, names, err := parseRecord(r.rec, nil)
		if err != nil {
			t.Fatalf("record %d: %v", r.no, err)
		}
		if info.inUse {
			m.add(r.no, info, names)
		}
	}
	return m
}

func TestBuildTree(t *testing.T) {
	const ft = 133_000_000_000_000_000
	root5 := ref(5, 5)
	recs := []numbered{
		// Extension record first and with a higher number than its base: order must not matter.
		{45, fileRecord(inUse, 1, ref(20, 7), nonResAttr(attrData, nonRes{lastVCN: 1, alloc: 8192, size: 5000, runs: []byte{0x11, 2, 9}}))},
		{0, fileRecord(inUse, 1, 0, stdInfo(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM, 0), fileNameAttr(root5, 3, "$MFT"),
			nonResAttr(attrData, nonRes{lastVCN: 0, alloc: 4096, size: 4096, runs: []byte{0x11, 1, 9}}))},
		{5, fileRecord(inUseDir, 5, 0, fileNameAttr(root5, 3, "."))},
		// "b" (record 30) sits inside "a" (record 40): the child has the smaller record number.
		{30, fileRecord(inUseDir, 2, 0, fileNameAttr(ref(40, 3), 3, "b"))},
		{40, fileRecord(inUseDir, 3, 0, fileNameAttr(root5, 3, "a"))},
		// Two hard links plus a DOS name; the data is in extension record 45.
		{20, fileRecord(inUse, 7, 0, stdInfo(windows.FILE_ATTRIBUTE_HIDDEN, ft),
			fileNameAttr(ref(30, 2), 1, "Report Long.txt"),
			fileNameAttr(ref(30, 2), nsDOS, "REPORT~1.TXT"),
			fileNameAttr(ref(40, 3), 0, "link.txt"))},
		{21, fileRecord(inUse, 1, 0, fileNameAttr(root5, 3, "small.txt"), residentData(100))},
		{22, fileRecord(inUse, 1, 0, stdInfo(windows.FILE_ATTRIBUTE_COMPRESSED, 0), fileNameAttr(ref(40, 3), 3, "comp.bin"),
			nonResAttr(attrData, nonRes{flags: dataCompressed, lastVCN: 15, alloc: 65536, size: 65536, total: 16384, runs: []byte{0x11, 4, 9, 0x01, 12}}))},
		{23, fileRecord(inUseDir, 1, 0, stdInfo(windows.FILE_ATTRIBUTE_REPARSE_POINT, 0), fileNameAttr(root5, 3, "j"),
			reparseAttr(windows.IO_REPARSE_TAG_MOUNT_POINT))},
		// Orphans: parent record free, parent sequence stale, a folder cycle, and a free record.
		{60, fileRecord(inUse, 1, 0, fileNameAttr(ref(70, 1), 3, "orphan"), residentData(1))},
		{61, fileRecord(inUse, 1, 0, fileNameAttr(ref(40, 9), 3, "stale"), residentData(1))},
		{80, fileRecord(inUseDir, 1, 0, fileNameAttr(ref(81, 1), 3, "x"))},
		{81, fileRecord(inUseDir, 1, 0, fileNameAttr(ref(80, 1), 3, "y"))},
		{62, fileRecord(0, 1, 0, fileNameAttr(root5, 3, "deleted"), residentData(1))},
		// A folder claiming a second parent: only its first sighting is kept.
		{90, fileRecord(inUseDir, 1, 0, fileNameAttr(root5, 3, "twice"), fileNameAttr(ref(40, 3), 3, "twice-again"))},
	}
	tr := buildTree(`C:\`, collect(t, 100, recs))
	tr.Finalize()

	for id := tree.ID(1); int(id) < tr.Len(); id++ {
		if tr.Parent(id) >= id {
			t.Fatalf("node %d (%s) has parent %d: parents must have smaller IDs", id, tr.Path(id), tr.Parent(id))
		}
	}
	if got, want := tr.Len(), 10; got != want {
		t.Errorf("node count = %d, want %d", got, want)
	}
	if got, want := tr.Alloc(0), uint64(4096+8192+16384); got != want {
		t.Errorf("root alloc = %d, want %d (hard link counted twice?)", got, want)
	}
	if got, want := tr.Size(0), uint64(4096+5000+100+65536); got != want {
		t.Errorf("root size = %d, want %d", got, want)
	}
	if got := tr.Files(0); got != 5 {
		t.Errorf("file count = %d, want 5 ($MFT, two links, small.txt, comp.bin)", got)
	}

	find := func(p string) tree.ID {
		t.Helper()
		id, ok := tr.Find(p)
		if !ok {
			t.Fatalf("%s missing", p)
		}
		return id
	}
	long, link := find(`C:\a\b\Report Long.txt`), find(`C:\a\link.txt`)
	if (tr.Flags(long)^tr.Flags(link))&tree.HardLink == 0 {
		t.Errorf("exactly one of the two links should be flagged HardLink")
	}
	if tr.Flags(long)&tr.Flags(link)&tree.Hidden == 0 {
		t.Errorf("both links should carry the file's Hidden attribute")
	}
	if tr.ModTime(long) != filetimeToUnix(ft) {
		t.Errorf("mtime = %d, want %d", tr.ModTime(long), filetimeToUnix(ft))
	}
	if f := tr.Flags(find(`C:\j`)); f&(tree.Dir|tree.Reparse) != tree.Dir|tree.Reparse {
		t.Errorf("junction flags = %b, want Dir|Reparse", f)
	}
	if f := tr.Flags(find(`C:\a\comp.bin`)); f&tree.Compressed == 0 || f&tree.Dir != 0 {
		t.Errorf("comp.bin flags = %b", f)
	}
	if f := tr.Flags(find(`C:\$MFT`)); f&(tree.Hidden|tree.System) != tree.Hidden|tree.System {
		t.Errorf("$MFT flags = %b", f)
	}
	if a := find(`C:\a`); tr.Size(a) != 65536+5000 || tr.Files(a) != 3 {
		t.Errorf("a: size %d files %d, want 70536 and 3", tr.Size(a), tr.Files(a))
	}
	find(`C:\twice`)
	for _, p := range []string{`C:\.`, `C:\a\b\REPORT~1.TXT`, `C:\orphan`, `C:\a\stale`, `C:\deleted`, `C:\a\twice-again`} {
		if _, ok := tr.Find(p); ok {
			t.Errorf("%s should not be in the tree", p)
		}
	}
	tr.Walk(0, func(id tree.ID) bool {
		if n := tr.Name(id); n == "x" || n == "y" || n == "orphan" || n == "stale" {
			t.Errorf("orphan %s reached the tree", n)
		}
		return true
	})
}

func TestBuildTreeWithoutRoot(t *testing.T) {
	m := collect(t, 10, []numbered{{6, fileRecord(inUse, 1, 0, fileNameAttr(ref(5, 5), 3, "f"), residentData(1))}})
	if tr := buildTree(`C:\`, m); tr.Len() != 1 {
		t.Errorf("tree without a root record has %d nodes, want only the root", tr.Len())
	}
}

// TestMalformedRecords feeds damaged and random records to every parser. Errors are
// fine; panics are not.
func TestMalformedRecords(t *testing.T) {
	valid := func() []byte {
		return fileRecord(inUse, 1, ref(3, 1), stdInfo(0, 0), fileNameAttr(ref(5, 5), 1, "name.txt"),
			nonResAttr(attrData, nonRes{flags: dataSparse, lastVCN: 3, alloc: 1, size: 1, total: 1, runs: []byte{0x11, 4, 9}}),
			reparseAttr(1))
	}
	m := newCollector(16)
	feed := func(rec []byte) {
		info, names, err := parseRecord(rec, nil)
		if err == nil && info.inUse {
			m.add(7, info, names)
		}
		_, _, _ = mftDataRuns(rec, 4096)
		_, _ = parseBoot(rec)
	}

	// Every truncation of a valid record.
	for n := range tRecSize {
		feed(valid()[:n])
	}
	// Every single-byte corruption, before and after the fixups are laid out.
	for i := range tRecSize {
		for _, v := range []byte{0x00, 0x7F, 0xFF} {
			rec := valid()
			rec[i] = v
			feed(rec)
		}
	}
	// Random bodies behind a plausible header with consistent fixups, so the attribute parser sees garbage.
	rng := rand.New(rand.NewPCG(1, 2))
	for range 5000 {
		rec := fileRecord(inUse, uint16(rng.IntN(8)), 0)
		for i := 0x38; i < tRecSize; i++ {
			rec[i] = byte(rng.UintN(256))
		}
		le.PutUint16(rec[0x14:], uint16(0x38+rng.IntN(64)))
		le.PutUint32(rec[0x18:], uint32(rng.IntN(tRecSize+64)))
		le.PutUint16(rec[510:], 0x0A0B)
		le.PutUint16(rec[1022:], 0x0A0B)
		feed(rec)
		_, _ = decodeRuns(rec[0x38:])
	}
	buildTree(`C:\`, m).Finalize()
}
