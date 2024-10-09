package dom

import (
	"bytes"
	"encoding/base64"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
)

// BrowserDownload triggers a download from the browser of the given data, named fileName with the given
// mimeType (the type protocol.MIMEType is a string, so you can easily convert mimetypes unknown to gonbui).
//
// It does that by creating a temporary link to download and faking a click on it, and later removing it.
//
// The data is converted to javascript in the page, that means that there is a large overhead, and that all
// data will be uploaded to the browser page (hence in the client's computer memory). This is totally fine
// for small files, but don't use this for large data.
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
//	dom.BrowserDownload("phonebook.csv", data, protocol.MIMEType("text/csv"))
func BrowserDownload(fileName string, data []byte, mimeType protocol.MIMEType) error {
	var b bytes.Buffer
	w := base64.NewEncoder(base64.StdEncoding, &b)
	if _, err := w.Write(data); err != nil {
		return errors.Wrapf(err, "failed to convert data to base64 in dom.BrowserDownload(%q, data, %q)", fileName, mimeType)
	}
	if err := w.Close(); err != nil {
		return errors.Wrapf(err, "failed to convert (Close) data to base64 in dom.BrowserDownload(%q, data, %q)", fileName, mimeType)
	}
	dataURL := "data:" + string(mimeType) + ";base64," + b.String()

	TransientJavascript(`var downloadLink = document.createElement('a');
downloadLink.href = '` + dataURL + `';
downloadLink.download = '` + fileName + `';
document.body.appendChild(downloadLink);
downloadLink.click();
document.body.removeChild(downloadLink);`)
	return nil
}
