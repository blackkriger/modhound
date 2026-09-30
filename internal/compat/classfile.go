package compat

import (
	"encoding/binary"
	"errors"
	"strings"
)

const (
	refField  = 'F'
	refMethod = 'M'
)

type ref struct {
	Kind  byte
	Owner string
	Name  string
	Desc  string
}

type class struct {
	Name    string
	Super   string
	Ifaces  []string
	Members []string
	Classes []string
	Refs    []ref
	ModIDs  []string
	Mod     string
	Deps    []string
}

var errBadClass = errors.New("not a class file")

type reader struct {
	b   []byte
	pos int
	err bool
}

func (r *reader) u1() int {
	if r.pos+1 > len(r.b) {
		r.err = true
		return 0
	}
	v := int(r.b[r.pos])
	r.pos++
	return v
}

func (r *reader) u2() int {
	if r.pos+2 > len(r.b) {
		r.err = true
		return 0
	}
	v := int(binary.BigEndian.Uint16(r.b[r.pos:]))
	r.pos += 2
	return v
}

func (r *reader) u4() int {
	if r.pos+4 > len(r.b) {
		r.err = true
		return 0
	}
	v := int(binary.BigEndian.Uint32(r.b[r.pos:]))
	r.pos += 4
	return v
}

func (r *reader) skip(n int) {
	if n < 0 || r.pos+n > len(r.b) {
		r.err = true
		return
	}
	r.pos += n
}

type entry struct {
	tag  int
	a, b int
	utf  string
}

func parseClass(data []byte) (*class, error) {
	r := &reader{b: data}
	if r.u4() != 0xCAFEBABE {
		return nil, errBadClass
	}
	r.skip(4)
	n := r.u2()
	pool := make([]entry, n)
	for i := 1; i < n && !r.err; i++ {
		tag := r.u1()
		e := entry{tag: tag}
		switch tag {
		case 1:
			l := r.u2()
			if r.pos+l <= len(r.b) {
				e.utf = string(r.b[r.pos : r.pos+l])
			}
			r.skip(l)
		case 3, 4:
			r.skip(4)
		case 5, 6:
			r.skip(8)
			pool[i] = e
			i++
			continue
		case 7, 8, 16, 19, 20:
			e.a = r.u2()
		case 9, 10, 11, 12, 17, 18:
			e.a, e.b = r.u2(), r.u2()
		case 15:
			e.a, e.b = r.u1(), r.u2()
		default:
			return nil, errBadClass
		}
		pool[i] = e
	}
	if r.err {
		return nil, errBadClass
	}
	utf := func(i int) string {
		if i <= 0 || i >= len(pool) {
			return ""
		}
		return pool[i].utf
	}
	className := func(i int) string {
		if i <= 0 || i >= len(pool) || pool[i].tag != 7 {
			return ""
		}
		return utf(pool[i].a)
	}
	c := &class{}
	for _, e := range pool {
		switch e.tag {
		case 7:
			if name := utf(e.a); name != "" && !strings.HasPrefix(name, "[") {
				c.Classes = append(c.Classes, name)
			}
		case 9, 10, 11:
			owner := className(e.a)
			if owner == "" || strings.HasPrefix(owner, "[") || e.b <= 0 || e.b >= len(pool) {
				continue
			}
			nt := pool[e.b]
			kind := byte(refMethod)
			if e.tag == 9 {
				kind = refField
			}
			c.Refs = append(c.Refs, ref{Kind: kind, Owner: owner, Name: utf(nt.a), Desc: utf(nt.b)})
		}
	}
	r.skip(2)
	c.Name = className(r.u2())
	c.Super = className(r.u2())
	for k := r.u2(); k > 0 && !r.err; k-- {
		c.Ifaces = append(c.Ifaces, className(r.u2()))
	}
	for _, kind := range []byte{refField, refMethod} {
		for k := r.u2(); k > 0 && !r.err; k-- {
			r.skip(2)
			name, desc := utf(r.u2()), utf(r.u2())
			c.Members = append(c.Members, member(kind, name, desc))
			skipAttributes(r)
		}
	}
	for k := r.u2(); k > 0 && !r.err; k-- {
		name := utf(r.u2())
		l := r.u4()
		end := r.pos + l
		if name == "RuntimeVisibleAnnotations" && end <= len(r.b) {
			c.ModIDs, c.Mod, c.Deps = modAnnotation(&reader{b: r.b[r.pos:end]}, utf)
		}
		r.pos = end
		if r.pos > len(r.b) {
			r.err = true
		}
	}
	if r.err || c.Name == "" {
		return nil, errBadClass
	}
	return c, nil
}

func member(kind byte, name, desc string) string {
	return string(kind) + name + " " + desc
}

func skipAttributes(r *reader) {
	for k := r.u2(); k > 0 && !r.err; k-- {
		r.skip(2)
		r.skip(r.u4())
	}
}

func modAnnotation(r *reader, utf func(int) string) (ids []string, mod string, deps []string) {
	for k := r.u2(); k > 0 && !r.err; k-- {
		typ := utf(r.u2())
		for p := r.u2(); p > 0 && !r.err; p-- {
			name := utf(r.u2())
			tag, value := elementValue(r, utf, 0)
			if tag != 's' || value == "" {
				continue
			}
			switch {
			case typ == "Lcpw/mods/fml/common/Mod;" && name == "modid":
				ids, mod = append(ids, value), value
			case typ == "Lcpw/mods/fml/common/Mod;" && name == "dependencies":
				deps = append(deps, value)
			case typ == "Lcpw/mods/fml/common/API;" && name == "provides":
				ids = append(ids, value)
			}
		}
	}
	return ids, mod, deps
}

const maxAnnotationDepth = 32

func elementValue(r *reader, utf func(int) string, depth int) (int, string) {
	if depth > maxAnnotationDepth {
		r.err = true
		return 0, ""
	}
	tag := r.u1()
	switch tag {
	case 'B', 'C', 'D', 'F', 'I', 'J', 'S', 'Z', 'c':
		r.u2()
	case 's':
		return tag, utf(r.u2())
	case 'e':
		r.u2()
		r.u2()
	case '@':
		r.u2()
		for p := r.u2(); p > 0 && !r.err; p-- {
			r.u2()
			elementValue(r, utf, depth+1)
		}
	case '[':
		for p := r.u2(); p > 0 && !r.err; p-- {
			elementValue(r, utf, depth+1)
		}
	default:
		r.err = true
	}
	return tag, ""
}
