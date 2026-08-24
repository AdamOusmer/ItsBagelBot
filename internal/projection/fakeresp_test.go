// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// RESP2 wire helpers for the fake valkey: header/bulk readers on the inbound
// side and the reply encoders on the outbound side. Split from the fake's
// command logic so each file stays a review of one concern (wire framing vs
// command semantics).

package projection

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// respCount reads one "*N"/"$N" header line and returns N.
func respCount(r *bufio.Reader, prefix byte) (int, error) {
	line, err := readLine(r)
	if err != nil {
		return 0, err
	}
	if len(line) == 0 || line[0] != prefix {
		return 0, fmt.Errorf("expected %q-prefixed header, got %q", prefix, line)
	}
	return strconv.Atoi(line[1:])
}

func readBulkString(r *bufio.Reader) (string, error) {
	size, err := respCount(r, '$')
	if err != nil {
		return "", err
	}
	buf := make([]byte, size+2) // payload + CRLF
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf[:size]), nil
}

func readRESPArray(r *bufio.Reader) ([]string, error) {
	n, err := respCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		arg, err := readBulkString(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func respSimple(s string) []byte { return []byte("+" + s + "\r\n") }
func respInt(v int64) []byte     { return []byte(":" + strconv.FormatInt(v, 10) + "\r\n") }
func respBulk(s string) []byte   { return []byte("$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n") }
func respNil() []byte            { return []byte("$-1\r\n") }
func respError(s string) []byte  { return []byte("-ERR " + s + "\r\n") }

func respFlatArray(items []string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(items))
	for _, s := range items {
		b.Write(respBulk(s))
	}
	return []byte(b.String())
}
