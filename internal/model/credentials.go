package model

// Cookie is a single name/value pair captured from the browser and sent back
// with the download's requests. Domain and Path are deliberately not stored:
// the pair is only ever replayed to the download's own URL.
type Cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Credentials are the per-download request secrets needed to fetch a
// protected file: cookies for a logged-in session, extra headers such as a
// non-default User-Agent, and the referring page.
//
// They are held by the Manager in memory for the lifetime of the download and
// are never written to the store, never logged, and never serialized to the
// API. All fields are tagged `json:"-"` so a Credentials value cannot leak
// into a response by accident.
type Credentials struct {
	// Headers are applied to every request for the download. The
	// downloader adds its own Range and User-Agent defaults unless these
	// override them.
	Headers map[string]string `json:"-"`

	// Cookies are joined into a single Cookie header, in order.
	Cookies []Cookie `json:"-"`

	// Referrer is sent as the Referer header. Empty means no referer.
	Referrer string `json:"-"`
}
