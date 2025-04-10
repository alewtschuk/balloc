package balloc

import (
	"log"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Define constants
const (
	DEFAULT_K  uint = 30 // default amount of memory that this memeory manager will manage unless explicitly set. This is calculated as 2^DEFAULT_K bytes
	MIN_K      uint = 20 // minimum size of the buddy memory pool
	MAX_K      uint = 48 // maximum size of the buddy memory pool. 1 larger than needed to allow indexed 1-N instead of 0-N. internal max memory is MAX_K-1
	SMALLEST_K uint = 6  // smallest memory block size that can be returned by the buddy_malloc. value must be large enough to account for the avail header

	BLOCK_AVAIL    uint16 = 1 // block is available to allocate
	BLOCK_RESERVED uint16 = 0 // block has been handed to user
	BLOCK_UNUSED   uint16 = 3 // block is unused completely
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

	// Evaluate and check default values
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
	var data []byte
	var err error
	data, err = unix.Mmap(-1, 0, int(pool.numBytes), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
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
	var firstBlock *Avail = (*Avail)(unsafe.Pointer(pool.base)) // cast raw memory to usable *Avail pointer
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
	var k uint = SMALLEST_K
	// Finds smallest k value that is >= bytes using bitshifting
	for (uintptr(1) << k) < bytes {
		k++
	}

	return k
}

// Calculate offset using go uintptr for pointer arithmetic workaround
func buddyCalc(pool *BuddyPool, block *Avail) *Avail {
	var offset uintptr = uintptr(unsafe.Pointer(block)) - pool.base // checks how far into the pool the block of memory is
	var buddyOffset uintptr = offset ^ (uintptr(1) << block.kval)   // flip the kth bit to get the buddy's pool location
	var buddyAddr uintptr = pool.base + buddyOffset                 // address of the buddy must be the distance of buddyOffset from the pool base

	return (*Avail)(unsafe.Pointer(buddyAddr))
}

// Mallocs the memory based on the requested size and the availability
// in the memory pool
func buddyMalloc(pool *BuddyPool, size uint) (unsafe.Pointer, error) {
	// Check if pool is nil
	if pool == nil || size == 0 {
		return nil, nil
	}

	// Lock malloc and defer unlock till function complete
	pool.lock.Lock()
	defer pool.lock.Unlock()

	// Get the correct kval (block size) for the request
	var k uint = btok(uintptr(size) + uintptr(unsafe.Sizeof(Avail{})))

	// Ensure k is the minimum smallest value we take
	if k < SMALLEST_K {
		k = SMALLEST_K
	}

	// Declare variable to track the kval of available non-self referenced blocks in the avail[k] list
	var availableK uint = k

	// Check if availableK is less than pool.kvalM and check if the current avail head node is empty (points to itself)
	// increment availableK to proceed through avail array in pool
	for availableK <= pool.kvalM && pool.avail[availableK].next == &pool.avail[availableK] {
		availableK++
	}

	// Check if availableK is larger than the pool kval and return nil
	// as no memory can be allocated
	if availableK > pool.kvalM {
		var err error = unix.ENOMEM
		log.Println("ERROR: No memory available to be allocated")
		return nil, err
	}

	// Remove a block from avail if there is a block that can be alloc'd at avail[availableK]
	var block *Avail = removeFirst(&pool.avail[availableK])

	// While availableK is greater than the correct kval decrement i by one
	for availableK > k {
		availableK -= 1
		// Split the block in avail into two
		var buddyOffset uintptr = uintptr(unsafe.Pointer(block)) + (uintptr(1) << availableK)
		var buddy *Avail = (*Avail)(unsafe.Pointer(buddyOffset))
		buddy.kval = uint16(availableK)
		buddy.tag = BLOCK_AVAIL
		insertBlock(&pool.avail[availableK], buddy)

		block.kval = uint16(availableK)
	}

	// Update block tag
	block.tag = BLOCK_RESERVED

	return unsafe.Pointer(uintptr(unsafe.Pointer(block)) + uintptr(unsafe.Sizeof(Avail{}))), nil

}

// Removes the first head node of an *Avail list
func removeFirst(head *Avail) *Avail {
	var first *Avail = head.next
	if first == head {
		return nil // list is empty
	}
	// Relink
	first.prev.next = first.next
	first.next.prev = first.prev

	// Wipe pointers for safety
	first.next = nil
	first.prev = nil

	return first
}

// Inserts the block into the head of the list of available blocks of its size
func insertBlock(head *Avail, block *Avail) {
	// Insert the block to the list: head <-> block <-> head.next
	block.next = head.next
	block.prev = head

	head.next.prev = block
	head.next = block
}

// Frees the block and its buddy
func buddyFree(pool *BuddyPool, ptr unsafe.Pointer) {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	// If pool and pointer is nil do nothing
	if pool == nil || ptr == nil {
		return
	}

	// Convert pointer to uintptr for pointer math
	var blockAddr uintptr = uintptr(ptr) - uintptr(unsafe.Sizeof(Avail{}))
	// Cast block address to ptr using unsafe.Pointer as an intermediary
	var block *Avail = (*Avail)(unsafe.Pointer(blockAddr))

	// Update block status and coalesce
	block.tag = BLOCK_AVAIL
	coalesce(pool, block)
}

// Attempt to merge this block with its buddy.
// Merging only occurs if both blocks are the same size (kval)
// and are both marked BLOCK_AVAIL. Coalescing continues
// recursively to form the largest free block possible.
func coalesce(pool *BuddyPool, block *Avail) {
	for {
		// Locate the buddy
		var buddy *Avail = buddyCalc(pool, block)

		// If buddy is outside pool range, bail
		var buddyPtr uintptr = uintptr(unsafe.Pointer(buddy))
		if buddyPtr < pool.base || buddyPtr >= pool.base+pool.numBytes {
			break // invalid memory: abort coalescing
		}

		// Check if the buddy is available or if its kvals are innequal(not in the same avail list/size)
		if buddy.tag != BLOCK_AVAIL || buddy.kval != block.kval {
			break
		}

		// Remove buddy from list. This is what ensures you have one larger block when merged
		// as you are destroying the reference to the buddy which will always be the XOR'd
		// compliment to the block
		buddy.prev.next = buddy.next
		buddy.next.prev = buddy.prev
		buddy.next = nil
		buddy.prev = nil

		// Lower address becomes the larger block
		var lowerBlock *Avail

		// Check which address is lower
		if uintptr(unsafe.Pointer(block)) < uintptr(unsafe.Pointer(buddy)) {
			lowerBlock = block // continue with block
		} else {
			lowerBlock = buddy // continue with buddy
		}

		// Merge
		lowerBlock.kval++  // Increment kval up i.e. going from two 512 byte blocks 2^9 to one 1024 byte block 2^10
		block = lowerBlock // Set the block passed to the function to the merged lowerBlock and updates target block
	}

	insertBlock(&pool.avail[block.kval], block) // insert coalesced block into its new avail[k] list
}

// Destroys and unmaps the memory pool
func buddyDestroy(pool *BuddyPool) error {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	const maxPoolSize = uintptr(1) << MAX_K

	// If there is no pool or base is 0, nothing can be destroyed
	if pool == nil || pool.base == 0 {
		return nil
	}

	// Get the pointer to the pool base to use for the unmap
	var dataPtr unsafe.Pointer = unsafe.Pointer(pool.base)

	// Unmaps the memory using byte slice cast as unix.Munmap expects []byte
	// Cast the dataPointer as a large slice to be trimmed (pretending this is the start of a lare array in memory)
	// Trims the length of the array to the size and capacity of pool.numBytes
	// uses go's three index slice syntax a[low : high : max] this means we
	// use a slice from 0 to pool.numBytes and no more or less than pool.numBytes
	// making an exact slice the memory range
	var err error = unix.Munmap((*[maxPoolSize]byte)(dataPtr)[:pool.numBytes:pool.numBytes])
	if err != nil {
		return err
	}

	// Zero the BuddyPool except the mutex lock so the defer can trigger sucessfullyc
	pool.base = 0
	pool.numBytes = 0
	pool.kvalM = 0
	for i := range pool.avail {
		pool.avail[i] = Avail{}
	}
	return nil
}
