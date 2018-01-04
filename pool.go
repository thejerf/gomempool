/*

Package gomempool implements a simple memory pool for byte slices.

When To Use gomempool

The Go garbage collector runs more often when more bytes are allocated.
(For full details, see the runtime package documentation on the GOGC
variable.) Avoiding allocations can help the stop-the-world GC run
much less often.

To determine if you should use this, first deploy your code into as
realistic an environment as possible. Extract a runtime.MemStats structure
from your running code. Examine the various Pause fields in that
structure to determine if you have a GC problem, preferably in conjunction
with some other monitoring of real performance. (Remember the numbers
are in nanoseconds.) If the numbers you have are OK for your use case,
STOP HERE. Do not proceed.

If you are generating a lot of garbage collection pauses, the next question
is why. Profile the heap. If the answer is anything other than []byte
slices, STOP HERE. Do not proceed.

Finally, if you are indeed seeing a lot of allocations of []bytes, you
may wish to preceed with using this library. gomempool is a power
tool; it can save your program, but it can blow your feet off, too.
(I've done both.)

That said, despite the narrow use case of this library, it can have an
effect in certain situations, which I happened to encounter. I suspect
the biggest use case is a network application that often allocates
large messages, which is what I have, causing an otherwise relatively
memory-svelte program to allocate dozens of megabytes per second of
[]byte buffers to process messages.

Using Gomempool

To use the pool, there are three basic steps:

 1. Create a pool
 2. Obtain byte slices
 3. Optionally return the byte slices

The following prose documentation will cover each step at length. Expand the
Example below for concise usage examples and things you can copy & paste.

Create A Pool

First, create a pool.

    pool := gomempool.New(
            64,          // minimum sized slice to store and hand out
            1024 * 1024, // maximum sized slice to store and hand out
            10,          // maximum number of buffers to save
        )

A *Pool is obtained by calling gomempool.New. All methods on the *Pool are
threadsafe. The pool can be configured via the New call.

    var pool *gomempool.Pool

A nil pointer of type *gomempool.Pool is also a valid pool. This will
use normal make() to create byte slices and simply discard the slice
when asked to .Return() it. This is convenient for testing whether
you've got a memory error in your code, because you can swap in a nil
Pool pointer without changing any other code. If an error goes away
when you do that, you have a memory error. (Probably .Return()ing
something too soon.)

Obtain byte slices

[]bytes in the pool come with an "Allocator" that is responsible for
returning them correctly.

    allocator := pool.GetNewAllocator()
    bytes := allocator.Allocate(453)

    // use the bytes

To obtain an allocator, you call GetNewAllocator() on the pool. This
returns an allocator that is not yet used. You may then call
.Allocate(uint64) on it to assign a []byte from the pool. This []byte
is then associated with that Allocator until you call .Return() on
the allocator, after which the []byte goes back into the pool.

Allocations never fail (barring a complete out-of-memory situation, of
course). If the pool does not have a correctly-sized []byte on hand,
it will create one.

If you ask for more bytes than the pool is configured to store, the
Allocator will create a transient []byte, which it will not manage.
You can check whether you are invoking this case by calling .MaxSize on
the pool.

You MUST NOT call .Return() until you are entirely done with the []byte.
This includes shared slices you may have created; this is by far the
easiest way to get in trouble with a []byte pool, as it is easy to
accidentally introduce sharing without realizing it.

You must also make sure not to do anything with your []byte that might
cause another []byte to be created instead; for instance, using
your pooled []byte in an "append" call is dangerous, because the runtime
might decide to give you back a []byte that backs to an entirely
different array. In this case your []byte and your Allocator will
cease to be related. If the Allocator is correctly managed, your code
will not fail, but you won't be getting any benefit, either.

    allocator2 := pool.GetNewAllocator()
    allocator2.Allocate(738)
    newBytes := allocator.Bytes()

You may retrieve the []byte slice corresponding to an Allocator at any
time by calling .Bytes(). This means that if you do need to pass the
[]byte and Allocator around, it suffices to pass just the Allocator.
(Though, be aware that the Allocator's []byte will be of the original
size you asked for. This can not be changed, as you can not change the
original slice itself.)

Allocators can be reused freely, as long as they are used correctly.
However, an individual Allocator is not threadsafe. Its interaction
with the Pool is, but its internal values are not; do not use the
same Allocator from more than one goroutine at a time.

    // using the same allocator as above
    allocator2.Allocate(723) // PANIC!

Once allocated, an allocator will PANIC if you try to allocate again with
ErrBytesAlreadyAllocated.

Once .Return() has been called, an allocator will PANIC if you try to
.Return() the []byte again.

If no []byte is currently allocated, .Bytes() will PANIC if called.

This is fully on purpose. All of these situations represent profound
errors in your code. This sort of error is just as dangerous in Go
as it is in any other language. Go may not segfault, but memory
management issues can still dish out the pain; better to find out
earlier rather than later.

    thirdBytes, newAllocator := pool.Allocate(23314)

You can combine obtaining an Allocator and a particular sized []byte
by calling .Allocate() on the pool.

The Allocators returned by the nil *Pool use make() to create new
slices every time, and simply discard the []byte when done, but they
enforce the exact same rules as the "real" Allocators, and panic
in all the same places. This is so there is as little difference as
possible between the two types of pools.

Optionally return the byte slices

    bytes, allocator := pool.Allocate(29348)
    defer allocator.Return()
    // use bytes

Calling .Return() on an allocator is optional. If a []byte is not
returned to the pool, you do not get any further benefit from the pool
for that []byte, but the garbage collector will still clean it up
normally. This means using a pool is still feasible even if some of
your code paths may need to retain a []byte for a long or complicated
period of time.

Best Usage

If you have a byte slice that is getting passed through some goroutines,
I recommend creating a structure that holds all the relevant data about
the object bound together with the allocator:

 type UsesBytesFromPool struct {
     alloc         gomempool.Allocator
     ParsedMessage ParsedMessage
}

which makes it easy to keep the two bound together, and pass them around,
with only the last user finally performing the deallocation.

This library does not provide this structure since all I could give you
is basically the above struct, with an interface{} in it.

Additional Functionality

You can query the pool for its cache statistics by calling Stats(),
which will return a structure describing how the individual buckets
are performing.

*/
package gomempool

import (
	"errors"
	"sync"
	"sync/atomic"
)

const (
	largestPowerWeSupport = 40
)

// This is "panic"ed out when attempting to Return() an Allocation that has
// already been returned.
var ErrBytesAlreadyReturned = errors.New("can't perform operation because the bytes were already returned")

// This is "panic"ed out when attempting to Allocate() with an Allocator
// that is currently still allocated.
var ErrBytesAlreadyAllocated = errors.New("can't allocate again, because this already has allocated bytes")

// A Pool implements the memory pool.
type Pool struct {
	// the index of the elements entry is computed from the size of the
	// allocation.
	elements []*poolElement
	maxSize  uint64
	minSize  uint64
	maxBufs  uint64
	stats    []Stat

	// one might be tempted to try to be finer grained about this,
	// but given the most likely access patterns, the same bucket is
	// likely to be slammed over and over again anyhow.
	m sync.Mutex
}

type poolElement struct {
	buffer *buffer
	depth  uint32
	next   *poolElement
}

// A Stat records statistics for memory allocations of the given
// .Size.
//
// A Hit is when a user requested an allocation of a certain size, and
// the Pool handed out a suitable memory chunk from what it had on hand.
//
// A Miss is when a user requested an allocation of a certain size, and
// the Pool had to create a new []byte due to not having anything on hand.
//
// A Returned count is the number of buffers that have been .Return()ed.
//
// A Discarded count means that the user .Return()ed, but the Pool
// already had its maximum number on hand of []bytes of the given size,
// so it has left the returned []byte to the tender mercies of the
// standard GC.
//
// The Depth is a snapshot of how deep the linked list of unused buffers
// currently is for this bucket.
type Stat struct {
	Size      uint64
	Hit       uint64
	Miss      uint64
	Returned  uint64
	Discarded uint64
	Depth     uint64
}

// New returns a new Pool.
//
// maxSize indicates the maximum size we're willing to allocate.
// The minSize is the min we're willing
// to allocate. maxBufs is the maximum number of buffers we're willing to
// save at a time. Max memory consumed by unused buffers is approx maxBufs
// * maxSize * 2.
func New(minSize, maxSize, maxBufs uint64) *Pool {
	if minSize > maxSize {
		panic("can't create a pool with minSize greater than the maxSize")
	}

	if maxSize >= 1<<largestPowerWeSupport {
		panic("this size pool is insane")
		// besides... seriously? You've got petabytes of RAM and you need
		// to hand out petabyte chunks (plural!) of buffers to avoid GC?
		// You've got bigger problems than I can solve.
	}

	stats := make([]Stat, largestPowerWeSupport)
	for idx := range stats {
		stats[idx].Size = 1 << uint(idx)
	}

	return &Pool{
		elements: make([]*poolElement, largestPowerWeSupport),
		stats:    stats,
		maxSize:  maxSize,
		minSize:  minSize,
		maxBufs:  maxBufs,
	}
}

// Stats atomically fetches the stats, and returns you an independent copy.
// It automatically filters out any buckets that have seen no activity.
func (p *Pool) Stats() []Stat {
	if p == nil {
		return []Stat{}
	}

	s := []Stat{}
	p.m.Lock()
	defer p.m.Unlock()

	for idx, stat := range p.stats {
		if stat.Hit+stat.Miss+stat.Discarded > 0 {
			element := p.elements[idx]
			if element == nil {
				stat.Depth = 0
			} else {
				stat.Depth = uint64(element.depth + 1)
			}
			s = append(s, stat)
		}
	}
	return s
}

// GetNewAllocator returns an Allocator that can be used to obtain byte slices.
func (p *Pool) GetNewAllocator() Allocator {
	if p == nil {
		return &gcAllocator{nil}
	}

	return &poolAllocator{nil, p}
}

// Allocate creates an Allocator, and also immediately obtains and returns
// a byte slice of the given size, associated with the returned Allocator.
func (p *Pool) Allocate(size uint64) ([]byte, Allocator) {
	alloc := p.GetNewAllocator()
	bytes := alloc.Allocate(size)
	return bytes, alloc
}

// MaxSize returns the maximum size of byte slices the pool will manage.
func (p *Pool) MaxSize() uint64 {
	return p.maxSize
}

// MinSize returns the minimum size of byte slices the pool will return.
func (p *Pool) MinSize() uint64 {
	return p.minSize
}

func (p *Pool) get(reqSize uint64) *buffer {
	size := reqSize

	if reqSize > p.maxSize {
		return &buffer{make([]byte, reqSize, reqSize), reqSize, true}
	}
	if reqSize < p.minSize {
		size = p.minSize
	}

	// the characteristic "buffer Bit" is the bit that, when set, provides
	// the next power-of-two size:
	// 0010 -> 2 (0010)
	// 0011 -> 3 (0100)
	// 0100 -> 3 (0100)
	// 0101 -> 4 (1000)
	bufBit := largestSet(size)
	if 1<<bufBit < size {
		bufBit++
	}

	p.m.Lock()
	elem := p.elements[bufBit]
	if elem != nil {
		p.elements[bufBit] = elem.next
	}
	p.m.Unlock()

	if elem != nil {
		p.stats[bufBit].hit()
		elem.buffer.size = reqSize
		return elem.buffer
	}

	p.stats[bufBit].miss()
	bufSize := uint64(1 << bufBit)
	if bufSize > p.maxSize {
		bufSize = p.maxSize
	}
	return &buffer{make([]byte, bufSize, bufSize), reqSize, false}
}

func (p *Pool) returnToPool(b *buffer) {
	if b.unmanaged {
		return
	}

	bufBit := largestSet(uint64(cap(b.buf)))
	if 1<<bufBit < len(b.buf) {
		bufBit++
	}

	p.stats[bufBit].returned()

	p.m.Lock()
	defer p.m.Unlock()

	elem := p.elements[bufBit]
	if elem == nil {
		p.elements[bufBit] = &poolElement{
			buffer: b,
			depth:  0,
			next:   nil,
		}
	} else {
		if uint64(elem.depth+1) < p.maxBufs {
			p.elements[bufBit] = &poolElement{
				buffer: b,
				depth:  elem.depth + 1,
				next:   elem,
			}
		} else {
			// deliberately do nothing; we're discarding and letting
			// the GC pick it up.
			p.stats[bufBit].discarded()
		}
	}
}

func (bs *Stat) hit() {
	atomic.AddUint64(&bs.Hit, 1)
}
func (bs *Stat) miss() {
	atomic.AddUint64(&bs.Miss, 1)
}
func (bs *Stat) discarded() {
	atomic.AddUint64(&bs.Discarded, 1)
}
func (bs *Stat) returned() {
	atomic.AddUint64(&bs.Returned, 1)
}

// A Allocator wraps some allocated bytes.
type Allocator interface {
	// Allocate allocates the requested number of bytes and returns the
	// correct []byte. Panics if this allocator is currently allocated.
	Allocate(uint64) []byte

	// Bytes returns the currently-allocated bytes, or panics if there
	// aren't any. This will represent the same range of memory as what
	// Allocate returned.
	Bytes() []byte

	// This deallocates the allocator. If a pool is being used, this
	// returns the []byte to the pool; if the nil pool is being used, this
	// simply drops the []byte reference and lets the GC pick it up.
	Return()
}

type poolAllocator struct {
	*buffer
	pool *Pool
}

func (pb *poolAllocator) Allocate(size uint64) []byte {
	if pb.buffer != nil {
		panic(ErrBytesAlreadyAllocated)
	}

	pb.buffer = pb.pool.get(size)
	return pb.buf[:size]
}

func (pb *poolAllocator) Bytes() []byte {
	if pb.buffer == nil {
		panic(ErrBytesAlreadyReturned)
	}
	return pb.buffer.buf[:pb.size]
}

func (pb *poolAllocator) Return() {
	if pb.buffer == nil {
		panic(ErrBytesAlreadyReturned)
	}
	pb.pool.returnToPool(pb.buffer)
	pb.buffer = nil
}

type buffer struct {
	buf       []byte
	size      uint64
	unmanaged bool
}

type gcAllocator struct {
	bytes []byte
}

func (gcb *gcAllocator) Allocate(size uint64) []byte {
	if gcb.bytes != nil {
		panic(ErrBytesAlreadyAllocated)
	}
	gcb.bytes = make([]byte, size, size)
	return gcb.bytes
}

func (gcb *gcAllocator) Bytes() []byte {
	if gcb.bytes == nil {
		panic(ErrBytesAlreadyReturned)
	}
	return gcb.bytes
}

func (gcb *gcAllocator) Return() {
	if gcb.bytes == nil {
		panic(ErrBytesAlreadyReturned)
	}
	gcb.bytes = nil
}

// http://graphics.stanford.edu/~seander/bithacks.html#RoundUpPowerOf2
// suitably modified to work on 64-bit
// yields garbage above 0x7fffffffffffffff, so, you know, try not to
// allocate pools that large.
// I'm so excited, 15 years programming and I've never had this good an
// excuse to get down and seriously bash bits.
func nextPowerOf2(v uint64) uint64 {
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v |= v >> 32
	v++

	return v
}

func largestSet(v64 uint64) (r byte) {
	if v64&0xffffffff00000000 != 0 {
		return largestSet32(uint32(v64>>32)) + 32
	}
	return largestSet32(uint32(v64))
}

var multiplyDeBruijnBitPosition = [32]byte{
	0, 9, 1, 10, 13, 21, 2, 29, 11, 14, 16, 18, 22, 25, 3, 30,
	8, 12, 20, 28, 15, 17, 24, 7, 19, 27, 23, 6, 26, 5, 4, 31,
}

func largestSet32(v uint32) (r byte) {
	// http://graphics.stanford.edu/~seander/bithacks.html#IntegerLogDeBruijn
	// Returns the index of the largest set bit.
	v |= v >> 1 // first round down to one less than a power of 2
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16

	r = multiplyDeBruijnBitPosition[(v*0x07c4acdd)>>27]
	return
}
