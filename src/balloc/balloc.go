package balloc

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Define constants
const (
	DEFAULT_K  uint = 30 //default amount of memory that this memeory manager will manage unless explicitly set. This is calculated as 2^DEFAULT_K bytes
	MIN_K      uint = 20 //minimum size of the buddy memory pool
	MAX_K      uint = 48 //maximum size of the buddy memory pool. 1 larger than needed to allow indexed 1-N instead of 0-N. internal max memory is MAX_K-1
	SMALLEST_K uint = 6  //smallers memory block size that can be returned by the buddy_malloc. value must be large enough to account for the avail header

	BLOCK_AVAIL    uint16 = 1 //block is available to allocate
	BLOCK_RESERVED uint16 = 0 //block has been handed to user
	BLOCK_UNUSED   uint16 = 3 //block is unused completely
)

// Represents one block in the free list
type Avail struct {
	tag  uint16 // tag for block status i.e. BLOCK_AVAIL, BLOCK_RESERVED
	kval uint16 // the k value of the block
	next *Avail // pointer to the next memory block
	prev *Avail // pointer to the last memory block
}

// Buddy memory pool.
// Tracks the whole region of memory we are managing
type BuddyPool struct {
	kvalM    uint         // the max kval of this pool, largest k we manage
	numBytes uintptr      // total number of bytes this pool manages
	base     uintptr      // the base address of mmap'd memory used for the buddy calculations
	avail    [MAX_K]Avail // the array of free available memory block headers set to an array of size MAX_K
	lock     sync.Mutex   // mutex lock for thread safety
}

func buddyInit(pool *BuddyPool, size uintptr) error {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	var kval uint
	if size == 0 {
		kval = DEFAULT_K
	} else {
		kval = btok(size)
	}

	if kval < MIN_K {
		kval = MIN_K
	}
	if kval > MAX_K {
		kval = MAX_K - 1
	}

	// Set kval and numBytes value using kval as offset
	pool.kvalM = kval
	pool.numBytes = uintptr(1) << pool.kvalM

	// Memory map a chunk of raw data we will manage
	data, err := unix.Mmap(-1, 0, int(pool.numBytes), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return err
	}
	// Saving base addr for pointer arithmetic later. Casting as go doesn't give raw pointers as default
	pool.base = uintptr(unsafe.Pointer(&data[0]))

	// Init the avail list and set all blocks to empty
	for i := range pool.avail {
		pool.avail[i].next = &pool.avail[i]
		pool.avail[i].prev = &pool.avail[i]
		pool.avail[i].kval = uint16(i)
		pool.avail[i].tag = BLOCK_UNUSED
	}

	// Setup the first block
	firstBlock := (*Avail)(unsafe.Pointer(pool.base)) // cast raw memory to usable *Avail pointer
	firstBlock.tag = BLOCK_AVAIL
	firstBlock.kval = uint16(kval)
	firstBlock.next = &pool.avail[kval]
	firstBlock.prev = &pool.avail[kval]

	// Set the sentinal node to point to/from first block
	// Now looks like: avail[kval] <-> firstBlock <-> avail[kval]
	pool.avail[kval].next = firstBlock
	pool.avail[kval].prev = firstBlock

	return nil
}

// Converts the given bytes to the equivalent k value
// such that 2^k is >= bytes
func btok(bytes uintptr) uint {
	// Init k to the smallest allowed size
	k := SMALLEST_K
	// Finds smallest k value that is >= bytes using bitshifting
	for (uintptr(1) << k) < bytes {
		k++
	}

	return k
}

func buddyCalc(pool *BuddyPool, block *Avail) *Avail {
	// Calculate offset using go uintptr for pointer arithmetic workaround
	offset := uintptr(unsafe.Pointer(block)) - pool.base // checks how far into the pool the block of memory is
	buddyOffset := offset ^ (uintptr(1) << block.kval)   // flip the kth bit to get the buddy's pool location
	buddyAddr := pool.base + buddyOffset                 // address of the buddy must be the distance of buddyOffset from the pool base

	return (*Avail)(unsafe.Pointer(buddyAddr))
}
