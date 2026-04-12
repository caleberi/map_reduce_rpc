// Plugin: urlextract
//
// Finds all HTTP/HTTPS URLs in the text, normalises them to lowercase,
// and counts how many times each unique URL appears across the corpus.
//
// Map emits (url, "1") for every URL token found.
// Reduce sums the counts.
//
// Build:
//
//	go build -buildmode=plugin \
//	  -o plugins/urlextract/urlextract.so \
//	  ./plugins/urlextract/urlextract.go

package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

// urlRE matches http:// and https:// URLs.
// It captures the URL up to the first whitespace or common trailing punctuation.
var urlRE = regexp.MustCompile(`https?://[^\s"'<>\[\](){},;]+`)

// Map scans the document contents for URL tokens.
func Map(_ string, contents string) []mrp.KeyValue {
	matches := urlRE.FindAllString(contents, -1)
	kvs := make([]mrp.KeyValue, 0, len(matches))
	for _, m := range matches {
		// Strip trailing punctuation that may have been captured.
		m = strings.TrimRight(m, ".,!?:")
		kvs = append(kvs, mrp.KeyValue{Key: strings.ToLower(m), Value: "1"})
	}
	return kvs
}

// Reduce sums occurrence counts for each URL.
func Reduce(_ string, values []string) string {
	total := 0
	for _, v := range values {
		n, err := strconv.Atoi(v)
		if err == nil {
			total += n
		}
	}
	return strconv.Itoa(total)
}
