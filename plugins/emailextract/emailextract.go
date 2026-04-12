// Plugin: emailextract
//
// Scans the input corpus for e-mail addresses and tallies how many times each
// unique address appears.  Addresses are normalised to lower-case.
//
// Build:
//
//	go build -buildmode=plugin \
//	  -o plugins/emailextract/emailextract.so \
//	  ./plugins/emailextract/emailextract.go

package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/caleberi/map_reduce_rpc/mrp"
)

// emailRE is a conservative e-mail address pattern.
var emailRE = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

// Map scans one document chunk for e-mail addresses.
func Map(_ string, contents string) []mrp.KeyValue {
	matches := emailRE.FindAllString(contents, -1)
	kvs := make([]mrp.KeyValue, 0, len(matches))
	for _, m := range matches {
		kvs = append(kvs, mrp.KeyValue{Key: strings.ToLower(m), Value: "1"})
	}
	return kvs
}

// Reduce sums occurrence counts for each e-mail address.
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
