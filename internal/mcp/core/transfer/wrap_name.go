package transfer

import (
	"net/http"
	"strings"
)

// WebsiteIndexFileName is the conventional entry-point filename for a static
// website served from an IPFS directory root. Gateways serve this file at the
// directory path.
const WebsiteIndexFileName = "index.html"

// sniffHeadLen is how many leading bytes are inspected to decide whether a
// wrapped single-file upload is HTML. 512 bytes matches http.DetectContentType.
const sniffHeadLen = 512

// ResolveWrappedFileName determines the filename to use for a single-file
// upload that is being wrapped in a directory root (website content), based on
// the first `head` bytes of the content.
//
// An explicit caller-supplied name is always honored. When no name was given
// (empty or DefaultUploadName) the leading bytes are sniffed: HTML content is
// named WebsiteIndexFileName ("index.html") so the wrapped site resolves at
// its root; otherwise "" is returned so the caller keeps its existing default
// (DefaultUploadName). The `wrap` guard means this never renames a bare
// (non-wrapped) upload.
func ResolveWrappedFileName(name string, wrap bool, head []byte) string {
	if !wrap {
		return name
	}
	if name != "" && name != DefaultUploadName {
		return name
	}
	if len(head) > sniffHeadLen {
		head = head[:sniffHeadLen]
	}
	if ct := http.DetectContentType(head); strings.HasPrefix(ct, "text/html") {
		return WebsiteIndexFileName
	}
	return ""
}
