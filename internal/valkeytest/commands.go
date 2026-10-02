// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkeytest

import (
	"strconv"
	"strings"
	"time"
)

func (a Args) Int(i int) int {
	n, _ := strconv.Atoi(a[i])
	return n
}

func (a Args) Option(i int) string {
	if i >= len(a) {
		return ""
	}
	return strings.ToUpper(a[i])
}

func defaultHandlers() map[string]Handler {
	return map[string]Handler{
		"GET":     reader(func(s *Server, a Args) (string, bool) { v, ok := s.Strings[a.Key()]; return v, ok }),
		"HGET":    reader(func(s *Server, a Args) (string, bool) { v, ok := s.Hashes[a.Key()][a[1]]; return v, ok }),
		"SET":     set,
		"DEL":     keyCounter(true),
		"EXISTS":  keyCounter(false),
		"EXPIRE":  expire,
		"TTL":     ttl,
		"HSET":    hset,
		"HMGET":   hmget,
		"HGETALL": hgetall,
		"HDEL":    hdel,
		"SADD":    sadd,
		"SCARD":   scard,
		"LPUSH":   lpush,
		"LTRIM":   ltrim,
		"LRANGE":  func(s *Server, a Args) Reply { return Flat(window(s, a)) },
	}
}

func reader(lookup func(s *Server, a Args) (string, bool)) Handler {
	return func(s *Server, a Args) Reply {
		value, found := lookup(s, a)
		if !s.Alive(a.Key()) || !found {
			return Nil()
		}
		return Bulk(value)
	}
}

func keyCounter(remove bool) Handler {
	return func(s *Server, a Args) Reply {
		n := int64(0)
		for _, key := range a {
			if s.Alive(key) {
				n++
			}
			if remove {
				s.drop(key)
			}
		}
		return Int(n)
	}
}

func set(s *Server, a Args) Reply {
	key := a.Key()
	s.drop(key)
	s.Strings[key] = a[1]
	for i := 2; i+1 < len(a); i += 2 {
		unit := map[string]time.Duration{"EX": time.Second, "PX": time.Millisecond}[a.Option(i)]
		if unit != 0 {
			s.Expires[key] = s.Now().Add(time.Duration(a.Int(i+1)) * unit)
		}
	}
	return Simple("OK")
}

func expire(s *Server, a Args) Reply {
	key := a.Key()
	if !s.Alive(key) {
		return Int(0)
	}
	target := s.Now().Add(time.Duration(a.Int(1)) * time.Second)
	current, has := s.Expires[key]
	blocked := (a.Option(2) == "NX" && has) || (a.Option(2) == "GT" && (!has || !target.After(current)))
	if blocked {
		return Int(0)
	}
	s.Expires[key] = target
	return Int(1)
}

func ttl(s *Server, a Args) Reply {
	deadline, ok := s.Expires[a.Key()]
	switch {
	case !s.Alive(a.Key()):
		return Int(-2)
	case !ok:
		return Int(-1)
	}
	return Int(int64((deadline.Sub(s.Now()) + time.Second - 1) / time.Second))
}

func hset(s *Server, a Args) Reply {
	key, pairs := a.Key(), a[1:]
	if len(pairs)%2 != 0 {
		return Err("wrong number of arguments for HSET")
	}
	s.Alive(key)
	if s.Hashes[key] == nil {
		s.Hashes[key] = map[string]string{}
	}
	added := int64(0)
	for i := 0; i < len(pairs); i += 2 {
		if _, exists := s.Hashes[key][pairs[i]]; !exists {
			added++
		}
		s.Hashes[key][pairs[i]] = pairs[i+1]
	}
	return Int(added)
}

func hmget(s *Server, a Args) Reply {
	live := s.Alive(a.Key())
	out := make([]Reply, 0, len(a)-1)
	for _, field := range a[1:] {
		v, ok := s.Hashes[a.Key()][field]
		if ok && live {
			out = append(out, Bulk(v))
			continue
		}
		out = append(out, Nil())
	}
	return Array(out)
}

func hgetall(s *Server, a Args) Reply {
	var flat []string
	if s.Alive(a.Key()) {
		for field, value := range s.Hashes[a.Key()] {
			flat = append(flat, field, value)
		}
	}
	return Flat(flat)
}

func hdel(s *Server, a Args) Reply {
	key := a.Key()
	if !s.Alive(key) {
		return Int(0)
	}
	removed := int64(0)
	for _, field := range a[1:] {
		if _, ok := s.Hashes[key][field]; ok {
			delete(s.Hashes[key], field)
			removed++
		}
	}
	if len(s.Hashes[key]) == 0 {
		s.drop(key)
	}
	return Int(removed)
}

func sadd(s *Server, a Args) Reply {
	key := a.Key()
	s.Alive(key)
	if s.Sets[key] == nil {
		s.Sets[key] = map[string]bool{}
	}
	added := int64(0)
	for _, member := range a[1:] {
		if !s.Sets[key][member] {
			s.Sets[key][member] = true
			added++
		}
	}
	return Int(added)
}

func scard(s *Server, a Args) Reply {
	if !s.Alive(a.Key()) {
		return Int(0)
	}
	return Int(int64(len(s.Sets[a.Key()])))
}

func lpush(s *Server, a Args) Reply {
	key := a.Key()
	s.Alive(key)
	for _, element := range a[1:] {
		s.Lists[key] = append([]string{element}, s.Lists[key]...)
	}
	return Int(int64(len(s.Lists[key])))
}

func ltrim(s *Server, a Args) Reply {
	kept := window(s, a)
	if len(kept) == 0 {
		s.drop(a.Key())
	} else {
		s.Lists[a.Key()] = kept
	}
	return Simple("OK")
}

func window(s *Server, a Args) []string {
	if !s.Alive(a.Key()) {
		return nil
	}
	list := s.Lists[a.Key()]
	start, stop := a.Int(1), a.Int(2)
	if start < 0 {
		start = max(len(list)+start, 0)
	}
	if stop < 0 {
		stop += len(list)
	}
	stop = min(stop, len(list)-1)
	if start > stop {
		return nil
	}
	return list[start : stop+1]
}
