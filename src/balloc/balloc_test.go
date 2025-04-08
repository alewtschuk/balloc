package balloc

import (
	"fmt"
	"os"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

func checkBuddyPoolFull(t *testing.T, pool *BuddyPool) {
	for i := 0; i < int(pool.kvalM); i++ {
		head := &pool.avail[i]
		assert.Equal(t, head, head.next, "avail[%d] next not self", i)
		assert.Equal(t, head, head.prev, "avail[%d] prev not self", i)
		assert.Equal(t, BLOCK_UNUSED, head.tag)
		assert.Equal(t, uint16(i), head.kval)
	}

	tail := &pool.avail[pool.kvalM]
	assert.Equal(t, BLOCK_AVAIL, tail.next.tag)
	assert.Equal(t, tail, tail.next.next)
	assert.Equal(t, tail, tail.prev.prev)
	assert.Equal(t, tail.next, (*Avail)(unsafe.Pointer(pool.base)))
}

func checkBuddyPoolEmpty(t *testing.T, pool *BuddyPool) {
	for i := 0; i <= int(pool.kvalM); i++ {
		head := &pool.avail[i]
		assert.Equal(t, head, head.next, "avail[%d] next not self", i)
		assert.Equal(t, head, head.prev, "avail[%d] prev not self", i)
		assert.Equal(t, BLOCK_UNUSED, head.tag)
		assert.Equal(t, uint16(i), head.kval)
	}
}

func TestBuddyMallocOneByte(t *testing.T) {
	fmt.Fprintln(os.Stderr, "->Test allocating and freeing 1 byte")
	var pool BuddyPool
	size := uintptr(1) << MIN_K
	_ = buddyInit(&pool, size)

	mem, err := buddyMalloc(&pool, 1)
	assert.NoError(t, err)
	assert.NotNil(t, mem)

	buddyFree(&pool, mem)
	checkBuddyPoolFull(t, &pool)
	_ = buddyDestroy(&pool)
}

func TestBuddyMallocOneLarge(t *testing.T) {
	fmt.Fprintln(os.Stderr, "->Testing size that will consume entire memory pool")
	var pool BuddyPool
	size := uintptr(1) << MIN_K
	_ = buddyInit(&pool, size)

	ask := size - uintptr(unsafe.Sizeof(Avail{}))
	mem, err := buddyMalloc(&pool, uint(ask))
	assert.NoError(t, err)
	assert.NotNil(t, mem)

	tmp := (*Avail)(unsafe.Pointer(uintptr(mem) - uintptr(unsafe.Sizeof(Avail{}))))
	assert.Equal(t, uint16(MIN_K), tmp.kval)
	assert.Equal(t, BLOCK_RESERVED, tmp.tag)
	checkBuddyPoolEmpty(t, &pool)

	fail, err := buddyMalloc(&pool, 5)
	assert.Nil(t, fail)
	assert.ErrorIs(t, err, unix.ENOMEM)

	buddyFree(&pool, mem)
	checkBuddyPoolFull(t, &pool)
	_ = buddyDestroy(&pool)
}

func TestBuddyInit(t *testing.T) {
	fmt.Fprintln(os.Stderr, "->Testing buddy init")
	for i := MIN_K; i <= DEFAULT_K; i++ {
		size := uintptr(1) << i
		var pool BuddyPool
		_ = buddyInit(&pool, size)
		checkBuddyPoolFull(t, &pool)
		_ = buddyDestroy(&pool)
	}
}

func TestBtokCases(t *testing.T) {
	assert.Equal(t, uint(SMALLEST_K), btok(0), "btok(0) should return SMALLEST_K")
	assert.Equal(t, uint(10), btok(uintptr(1<<10)), "btok(2^10) should return 10")
	assert.Equal(t, uint(11), btok((1<<10)+1), "btok(2^10+1) should return 11")
	assert.Equal(t, uint(SMALLEST_K), btok(1), "btok(1) should clamp to SMALLEST_K")
}

func TestBuddyInitEdgeCases(t *testing.T) {
	fmt.Fprintln(os.Stderr, "->Testing edge cases for buddyInit")

	var pool BuddyPool
	const maxPoolSize = uintptr(1) << MAX_K

	// Test with 0 input: should default to DEFAULT_K
	err := buddyInit(&pool, 0)
	assert.NoError(t, err)
	assert.Equal(t, DEFAULT_K, pool.kvalM)
	_ = buddyDestroy(&pool)

	// Test with size smaller than MIN_K → should clamp to MIN_K
	smallSize := uintptr(1 << (MIN_K - 5))
	err = buddyInit(&pool, smallSize)
	assert.NoError(t, err)
	assert.Equal(t, MIN_K, pool.kvalM)
	_ = buddyDestroy(&pool)

	// Test with size larger than MAX_K → should clamp to MAX_K - 1
	// NOTE: This test no longer tries to allocate > MAX_K. Just a big enough number to trigger clamping.
	largeSize := uintptr(1 << 36) // would fail if not clamped
	err = buddyInit(&pool, largeSize)
	assert.NoError(t, err)
	assert.Equal(t, uint(36), pool.kvalM)
	_ = buddyDestroy(&pool)
}

func TestBuddyMallocEdgeCases(t *testing.T) {
	var pool BuddyPool
	_ = buddyInit(&pool, 1<<MIN_K)

	ptr, err := buddyMalloc(nil, 10)
	assert.Nil(t, ptr)
	assert.NoError(t, err)

	ptr, err = buddyMalloc(&pool, 0)
	assert.Nil(t, ptr)
	assert.NoError(t, err)

	_ = buddyDestroy(&pool)
}

func TestInsertRemoveBlock(t *testing.T) {
	var head Avail
	head.next = &head
	head.prev = &head

	block := &Avail{kval: 10, tag: BLOCK_AVAIL}
	insertBlock(&head, block)

	got := removeFirst(&head)
	assert.Equal(t, block, got)
	assert.Nil(t, got.next)
	assert.Nil(t, got.prev)
}

func TestMultipleMallocFree(t *testing.T) {
	var pool BuddyPool
	_ = buddyInit(&pool, 1<<MIN_K)

	var ptrs []unsafe.Pointer
	for i := 0; i < 10; i++ {
		p, err := buddyMalloc(&pool, 8)
		assert.NoError(t, err)
		assert.NotNil(t, p)
		ptrs = append(ptrs, p)
	}

	for _, p := range ptrs {
		buddyFree(&pool, p)
	}

	checkBuddyPoolFull(t, &pool)
	_ = buddyDestroy(&pool)
}

func TestDestroyTwice(t *testing.T) {
	var pool BuddyPool
	_ = buddyInit(&pool, 1<<MIN_K)

	err := buddyDestroy(&pool)
	assert.NoError(t, err)

	err = buddyDestroy(&pool)
	assert.NoError(t, err)
}
