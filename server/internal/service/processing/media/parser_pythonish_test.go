package media

import "testing"

func TestExtractHDRInfoFromMediaTextKeepsHDR10(t *testing.T) {
	text := `
General
Complete name                            : sample.mkv

Video
Format                                   : HEVC
HDR format                               : SMPTE ST 2086, HDR10 compatible
Transfer characteristics                 : PQ
Color primaries                          : BT.2020
`

	hdr := ExtractHDRInfoFromMediaText(text, false)
	if hdr.StandardTag != "HDR10" {
		t.Fatalf("expected HDR10, got %q", hdr.StandardTag)
	}
}

func TestExtractHDRInfoFromBDInfoKeepsHDR10(t *testing.T) {
	text := `
DISC INFO:

PLAYLIST REPORT:

VIDEO:
Codec Bitrate Description
----- ------- -----------
MPEG-H HEVC Video 65000 kbps 2160p / 23.976 fps / 16:9 / Main 10 / HDR10
`

	hdr := ExtractHDRInfoFromMediaText(text, true)
	if hdr.StandardTag != "HDR10" {
		t.Fatalf("expected HDR10, got %q", hdr.StandardTag)
	}
}
