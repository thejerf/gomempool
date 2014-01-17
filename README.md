# gomempool

[![Build Status](https://travis-ci.org/thejerf/gomempool.png?branch=master)](https://travis-ci.org/thejerf/gomempool)

A []byte pool implementation for Go.

Go network programs can get into a bit of garbage collection trouble if
they are constantly allocating buffers of []byte to process network
requests. This library gives you an interface you can program to to
easily re-use []bytes without too much additional work. It also
provides statistics so you can query how well the Pool is working for you.

This is fully covered with [godoc](http://godoc.org/github.com/thejerf/gomempool),
including examples, motivation, and everything else you might otherwise
expect from a README.md on GitHub. (DRY.)

This is not currently tagged with particular git tags for Go as this is
currently considered to be alpha code. As I move this into production and
feel more confident about it, I'll give it relevant tags.

"What about sync.Pool?" At least as it stands now (Jan 17, 2014), I
believe sync.Pool actually complements this library, rather than supercedes
it. sync.Pool in its alpha state appears to more efficiently pool a 
homogeneous collection of objects, whereas the focus of gomempool here
is explicitly on heterogeneous sizes of []byte. If sync.Pool ships with
the same interface it has now, I will be able to use that as my backing
store instead of a linked list, and I believe only very small API changes
will be necessary, entirely located at the point of the gomempool.New call.
Unless they change the design of sync.Pool to more explicitly target
the heterogeneous case, there's no reason to wait on using this, if
this library meets your needs now.
