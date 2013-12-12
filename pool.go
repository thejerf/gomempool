/*

Package gomempool implements a simple memory pool for byte slices.

The Go garbage collector decides when to run by measuring the number of
bytes allocated. If you have a use pattern where you are allocating
a large number of sizeable []bytes, you will cause the garbage collector
to be run often. You can avoid this problem by reusing []bytes. This
package provides you a pool of []bytes to make this easier.

To use the pool, a memory Pool is created with New. You then request
an Allocator from the Pool, which will hand you back a []byte with
its Allocate call. When you are done with the []byte, call .Return()
on the Allocator. You may only have one []byte at a time from a
given Allocator, and you may not .Return() an Allocator that does
not currently have an associated []byte. A convenient all-in-one
function Allocate is also provided on the Pool object itself to both
return a []byte and its associated Allocator in one shot.

This Pool is surprisingly easy to use, because calling .Return() is
optional. If you don't return any buffers, you don't get any benefit
from the Pool, but Pools only use resources proportional to the values
it is waiting to hand out, so it's not that expensive, either. This
flexibility allows you to still benefit from a Pool even if you have
the occasional buffer that has a complicated lifetime; if that happens,
simply let the normal GC handle that buffer and the Pool will not break.

The Pool itself is threadsafe; the Allocators it hands out are not.

The library will panic if you misuse the Allocators. I have made
the unusual choice to panic out of a library because this represents a
memory management error, which is only marginally less serious in Go
than it is in any other language. (You may not segfault, but flawed
memory management can dish out far worse. Better to find out early.) This
code also panics if you request a buffer larger than the pool permits;
this I will admit is more dubious but adds errors to an awful lot of
signatures if we try to return errors. (If you are using a pool but
don't have some concept of "maximum buffer I'll support", you might want
to see if you can set such a size; unbounded buffers are often a DOS
opportunity.)

You can query the pool for its cache statistics by calling Stats(),
which will return a structure describing how the individual buckets
are performing.

A nil pointer of type pool.Pool provides a null Pool implementation,
where all allocations are served directly out of "make" and there's
no pooling. This can be useful to stub out the Pool for testing, or
comparing performance with and without the pool by only changing
where the Pool is created.

Quality: At the moment I would call this alpha code. Go lint clean, go vet
clean, 100% coverage in the tests. You and I both know that doesn't prove
this is bug-free, but at least it shows I care.

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
var ErrBytesAlreadyReturned = errors.New("can't .Return() the bytes from an Allocator because the bytes were already returned")

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
	Depth     uint32
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
		panic("can't create a pool with minSize less than the maxSize")
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

	// in order to keep this simple, we just go ahead and allocate all
	// largestPowerWeSupport possible "elements" in the pool, rather than
	// add lots of fiddly, bug-prone math to save a bare handful of bytes
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

	s := make([]Stat, 0)
	p.m.Lock()
	defer p.m.Unlock()

	for idx, stat := range p.stats {
		if stat.Hit+stat.Miss+stat.Discarded > 0 {
			element := p.elements[idx]
			if element == nil {
				stat.Depth = 0
			} else {
				stat.Depth = element.depth + 1
			}
			s = append(s, stat)
		}
	}
	return s
}

// GetNewAllocator returns an Allocator that can be used to obtain byte slices.
func (p *Pool) GetNewAllocator() Allocator {
	if p == nil {
		return &GCAllocator{nil}
	}

	return &PoolAllocator{nil, p}
}

// Allocate an Allocator, and also immediately obtains and returns a byte
// slice of the given size.
//
// The capacity of the resulting slice may be larger than what you asked
// for. It is safe to use this additional capacity if you choose.
func (p *Pool) Allocate(size uint64) ([]byte, Allocator) {
	alloc := p.GetNewAllocator()
	bytes := alloc.Allocate(size)
	return bytes, alloc
}

func (p *Pool) get(reqSize uint64) *buffer {
	size := reqSize

	if reqSize > p.maxSize {
		panic("can't fill request for buffer because it is larger than the pool will handle")
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
		elem.buffer.size = int(reqSize)
		return elem.buffer
	}

	p.stats[bufBit].miss()
	bufSize := uint64(1 << bufBit)
	if bufSize > p.maxSize {
		bufSize = p.maxSize
	}
	return &buffer{make([]byte, bufSize, bufSize), int(reqSize)}
}

func (p *Pool) returnToPool(b *buffer) {
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
	// Allocate allocates the requested number of bytes into the
	// ReturnableBytes fulfiller, or panics if this is already allocated.
	// Returns the byte slice that was allocated.
	//
	// The only error that can be returned is ErrAllocationTooLarge.
	Allocate(uint64) []byte

	// Bytes returns the currently-allocated bytes, or panics if there
	// aren't any. This will represent the same range of memory as what
	// Allocate returned, but the slice may not compare ==.
	Bytes() []byte

	// Returns the bytes to the pool, or whatever this interface does
	// to deallocate bytes. Panics with ErrBytesAlreadyReturned if
	// the bytes have already been returned.
	Return()
}

// PoolAllocator implements the Allocator interface, and is what is given
// to you by a non-nil Pool.
//
// PoolAllocator validates that the underlying buffer has not been .Return()ed
// at the point in time where you call .Bytes() or .Return(). However, this
// can not catch all possible sharing problems.
type PoolAllocator struct {
	*buffer
	pool *Pool
}

// Allocate implements the Allocator interface.
func (pb *PoolAllocator) Allocate(size uint64) []byte {
	if pb.buffer != nil {
		panic(ErrBytesAlreadyAllocated)
	}

	pb.buffer = pb.pool.get(size)
	return pb.buf[:size]
}

// Bytes implements the Allocator interface.
func (pb *PoolAllocator) Bytes() []byte {
	if pb.buffer == nil {
		panic(ErrBytesAlreadyReturned)
	}
	return pb.buffer.buf[:pb.size]
}

// Return implements the Allocator interface.
func (pb *PoolAllocator) Return() {
	if pb.buffer == nil {
		panic(ErrBytesAlreadyReturned)
	}
	pb.pool.returnToPool(pb.buffer)
	pb.buffer = nil
}

type buffer struct {
	buf  []byte
	size int
}

// GCAllocator serve up byte slices that are only managed by the conventional
// Go garbage collection. These are returned by the nil pool.
type GCAllocator struct {
	bytes []byte
}

// Allocate uses the normal make call to create a slice of bytes with a length
// and capacity of the given size.
func (gcb *GCAllocator) Allocate(size uint64) []byte {
	if gcb.bytes != nil {
		panic(ErrBytesAlreadyAllocated)
	}
	gcb.bytes = make([]byte, size, size)
	return gcb.bytes
}

// Bytes returns the []byte created by Allocate.
func (gcb *GCAllocator) Bytes() []byte {
	if gcb.bytes == nil {
		panic(ErrBytesAlreadyReturned)
	}
	return gcb.bytes
}

// Return "returns" the byte slice by releasing its reference to the
// []byte that it created.
//
// The Allocator contract is that once Return()ed, the corresponding byte
// slice will be reused; of course in this case that won't happen, but from
// the Pool's point of view that's an accident of implementation.
func (gcb *GCAllocator) Return() {
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

// Fun todo: Track down how to create a 64-bit version of the 32-bit function.
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
