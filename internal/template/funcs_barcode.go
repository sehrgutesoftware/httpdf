package template

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"text/template"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
)

func barcodeTemplateFuncs(funcs template.FuncMap) {
	funcs["qrCode"] = qrCodeFunc
}

// qrCodeFunc generates a QR code from the given data and returns it as a data
// URL containing a PNG image.
func qrCodeFunc(size int, data string) string {
	if size <= 0 {
		size = 256 // default size
	}

	qrCode, _ := qr.Encode(data, qr.M, qr.Auto)
	qrCode, _ = barcode.Scale(qrCode, size, size)
	buf := bytes.NewBuffer(nil)
	png.Encode(buf, qrCode)

	return "data:image/png;base64," + base64.RawStdEncoding.EncodeToString(buf.Bytes())
}
