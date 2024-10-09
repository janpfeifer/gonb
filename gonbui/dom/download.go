package dom

import (
	"bytes"
	"encoding/base64"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
)

// SendAsDownload sends data from a cell to the client by triggering a browser download.
//
// This function initiates a file download in the user's browser. It works by
// creating a temporary link with the provided data and simulating a click on it.
// The link is then removed.
//
// Note: This function encodes the data into the webpage, which is inefficient
// for large files. It's best suited for downloading smaller files.
//
// Parameters:
//   - fileName: The name of the downloaded file.
//   - data: The file content as a byte array.
//   - mimeType: The MIME type of the file (e.g., "text/csv", "image/png").
//
// Example:
//
//	lines := []string{"name,phone"}  // Header
//	nameToPhone := map[string]string{
//		"SpiderMan": "+1 407 224-1783",
//		"SandMan": "+44 20 999 123 456",
//		"Wanda": "+1 732 555 0125",
//	}
//	for name, phone := range nameToPhone {
//		lines = append(lines, fmt.Sprintf("%q,%q", name, phone))
//	}
//	data := []byte(strings.Join(lines, "\n"))
//	dom.SendAsDownload("phonebook.csv", data, "text/csv")
func SendAsDownload(fileName string, data []byte, mimeType protocol.MIMEType) {
	var b bytes.Buffer
	w := base64.NewEncoder(base64.StdEncoding, &b)
	if _, err := w.Write(data); err != nil {
		// bytes.Buffer.Write never returns an error.
		panic(errors.Wrapf(err, "failed to write to bytes.Buffer, this should never happen -- in dom.SendAsDownload(%q, data, %q)", fileName, mimeType))
	}
	if err := w.Close(); err != nil {
		// bytes.Buffer.Close never returns an error.
		panic(errors.Wrapf(err, "failed to close bytes.Buffer, this should never happen -- in dom.SendAsDownload(%q, data, %q)", fileName, mimeType))
	}
	dataURL := "data:" + string(mimeType) + ";base64," + b.String()

	TransientJavascript(`var downloadLink = document.createElement('a');
downloadLink.href = '` + dataURL + `';
downloadLink.download = '` + fileName + `';
document.body.appendChild(downloadLink);
downloadLink.click();
document.body.removeChild(downloadLink);`)
}
