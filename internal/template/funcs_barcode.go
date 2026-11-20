package template

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"image/png"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
)

func barcodeTemplateFuncs(funcs template.FuncMap) {
	funcs["qrCode"] = qrCodeFunc
}

// qrCodeFunc generates a QR code from the given data and returns it as a data
// URL containing a PNG image. It's typed as a URL, because html/template would
// otherwise replace the data URL with a placeholder in src attributes.
func qrCodeFunc(size int, data string) (template.URL, error) {
	if size <= 0 {
		size = 256 // default size
	}

	// With automatic encoding, the data can only fail to encode if it's too
	// long. The library's error is discarded, because it contains the data.
	qrCode, err := qr.Encode(data, qr.M, qr.Auto)
	if err != nil {
		return "", fmt.Errorf("qrCode: data is too long for a QR code (%d bytes)", len(data))
	}
	if minSize := qrCode.Bounds().Dx(); size < minSize {
		return "", fmt.Errorf("qrCode: size %d is too small for the data, it needs at least %d", size, minSize)
	}
	scaled, err := barcode.Scale(qrCode, size, size)
	if err != nil {
		return "", fmt.Errorf("qrCode: %w", err)
	}
	buf := bytes.NewBuffer(nil)
	if err := png.Encode(buf, scaled); err != nil {
		return "", fmt.Errorf("qrCode: %w", err)
	}

	return template.URL("data:image/png;base64," + base64.RawStdEncoding.EncodeToString(buf.Bytes())), nil
}
