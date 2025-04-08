package balloc

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"
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

func TestMain(m *testing.M) {
	rand.Seed(time.Now().UnixNano())
	fmt.Println("Running memory tests.")
	os.Exit(m.Run())
}
