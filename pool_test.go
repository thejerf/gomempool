package gomempool

import (
	"reflect"
	"testing"
)

func Example() {
	pool := New(4096, 10*1024*1024, 20)

	// simple usage
	bytes, alloc := pool.Allocate(12345)
	bytes[0] = 0

	// use bytes
	alloc.Return()
	// stop using bytes now, it could become anything

	// the nil Pool pointer has an implementation as well,
	// which uses normal garbage collection.
	pool = nil
	bytes, alloc = pool.Allocate(12345)
	alloc.Return()

	// You can also disconnect the act of obtaining an Allocator
	// from actually allocating. Presumably, one might pass this to
	// a function or something.
	alloc = pool.GetNewAllocator()
	bytes = alloc.Allocate(12345)
	defer alloc.Return()

	// ... use bytes...
}

type BitTest struct {
	in  uint64
	out uint64
}

func TestBitBashing(t *testing.T) {
	nextPower := []BitTest{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{8, 8},
		{9, 16},
		{15, 16},
		{16, 16},
		{1 << 30, 1 << 30},
		{1<<30 + 1, 1 << 31},
		{1<<32 - 1, 1 << 32},

		{1 << 32, 1 << 32},
		{1<<32 + 1, 1 << 33},

		{1<<60 - 1, 1 << 60},
		{1 << 60, 1 << 60},
		{1<<60 + 1, 1 << 61},
		{0x7fffffffffffffff, 1 << 63},
	}

	for _, test := range nextPower {
		if nextPowerOf2(test.in) != test.out {
			t.Fatal("nextPowerOf2 does not work as expected for", test.in, ", got", test.out)
		}
	}

	largestSetTest := []BitTest{
		{0, 0},
		{1, 0},
		{2, 1},
		{3, 1},
		{4, 2},
		{7, 2},
		{8, 3},
		{9, 3},
		{15, 3},
		{16, 4},
		{17, 4},
		{1<<30 - 1, 29},
		{1 << 30, 30},
		{1<<30 + 1, 30},

		{1<<32 - 1, 31},
		{1 << 32, 32},
		{1<<32 + 1, 32},

		{1<<60 - 1, 59},
		{1 << 60, 60},
		{1<<60 + 1, 60},
		{1 << 63, 63},
		{0xffffffffffffffff, 63},
	}

	for _, test := range largestSetTest {
		ls := largestSet(test.in)
		if ls != byte(test.out) {
			t.Fatal("largestSet does not work as expected: ", test.in, "yielded:", ls, "instead of", test.out)
		}
	}
}

func TestPool(t *testing.T) {
	p := New(256, 10*1024*1024, 3)

	// let's buf!
	b1, a1 := p.Allocate(1)
	if cap(b1) != 256 {
		t.Fatal("Pool unexpectedly not rounding up the way we expected.")
	}
	_, a2 := p.Allocate(1)
	b3, a3 := p.Allocate(1)
	_, a4 := p.Allocate(1)

	a1.Return()

	elem := p.elements[8]
	if elem.next != nil || elem.depth != 0 {
		t.Fatal("Returning element did not work as expected")
	}

	b3[0] = 65

	a2.Return()
	a3.Return()
	a4.Return()

	elem = p.elements[8]
	if elem.next == nil || elem.depth != 2 {
		t.Fatal("Returning elements did not work as expected", elem.depth)
	}

	b5, a5 := p.Allocate(10)
	if b5[0] != 65 {
		t.Fatal("Did not recover the expected buffer")
	}
	b5a := a5.Bytes()
	if b5a[0] != 65 {
		t.Fatal(".Bytes() did not return the proper byte slice")
	}

	// now let's verify my understanding of Go semantics, which is that
	// we should get a *copy* of the slice via .Bytes(), and nothing
	// the client does should be able to mangle the original Buffer's value
	b5 = b5[:1]

	a5.Return()

	b6, a6 := p.Allocate(1)
	if b6[0] != 65 || cap(b6) != 256 {
		t.Fatal("Buffers are not as independent as I thought!")
	}
	if len(b6) != 1 {
		t.Fatal("pool does not trim the slice properly.", len(b6))
	}

	b7, a7 := p.Allocate(10 * 1024 * 1024)
	if cap(b7) != 10*1024*1024 {
		t.Fatal("Rounding off the top end doesn't seem to work.")
	}
	a7.Return()
	b7 = a7.Allocate(10*1024*1024 - 1)
	if cap(b7) != 10*1024*1024 {
		t.Fatal("Rounding off the top end doesn't seem to work redux.")
	}
	a6.Return()
	a7.Return()

	p.Stats()
}

func TestNilPool(t *testing.T) {
	var p *Pool

	b, a := p.Allocate(25)

	if cap(b) != 25 {
		t.Fatal("Nil pool not working as expected")
	}

	// test this doesn't crash
	a.Return()

	p.Stats()
}

func TestStats(t *testing.T) {
	p := New(4, 16, 1)

	_, a := p.Allocate(8)

	if !reflect.DeepEqual(p.Stats(), []Stat{Stat{Size: 8, Hit: 0, Miss: 1, Returned: 0, Discarded: 0, Depth: 0}}) {
		t.Fatal("Stats not working properly.")
	}

	a.Return()
	_, a = p.Allocate(8)

	if !reflect.DeepEqual(p.Stats(), []Stat{Stat{Size: 8, Hit: 1, Miss: 1, Returned: 1, Discarded: 0, Depth: 0}}) {
		t.Fatal("Stats not working properly.")
	}

	_, a2 := p.Allocate(8)
	a.Return()
	a2.Return()

	if !reflect.DeepEqual(p.Stats(), []Stat{Stat{Size: 8, Hit: 1, Miss: 2, Returned: 3, Discarded: 1, Depth: 1}}) {
		t.Fatal("Stats not working properly.")
	}

}

// leftover bits we need to test
func TestCoverage(t *testing.T) {
	crashes := func(f func()) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("Failed to crash")
			}
		}()

		f()
	}

	crashes(func() {
		New(1, 1<<largestPowerWeSupport, 20)
	})

	crashes(func() {
		New(75, 50, 20)
	})

	crashes(func() {
		p := New(4, 16, 10)
		p.Allocate(17)
	})

	crashes(func() {
		p := New(4, 16, 1)
		_, a := p.Allocate(10)
		a.Allocate(10)
	})

	crashes(func() {
		p := New(4, 16, 1)
		a := p.GetNewAllocator()
		a.Bytes()
	})

	crashes(func() {
		p := New(4, 16, 1)
		a := p.GetNewAllocator()
		a.Return()
	})

	crashes(func() {
		p := New(4, 16, 1)
		_, a := p.Allocate(10)
		a.Return()
		a.Return()
	})

	// Ensure the gcAllocator faithfully maintains the interface
	// of the Allocator.
	crashes(func() {
		g := &gcAllocator{}
		g.Bytes()
	})

	crashes(func() {
		g := &gcAllocator{}
		g.Allocate(10)
		g.Allocate(10)
	})

	crashes(func() {
		g := &gcAllocator{}
		g.Return()
	})

	g := &gcAllocator{}
	g.Allocate(10)
	b := g.Bytes()
	if len(b) != 10 {
		t.Fatal("gcAllocator doesn't give out the right stuff.")
	}
}
