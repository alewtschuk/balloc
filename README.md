# Balloc: Buddy Memory Allocator in Go

## Overview

Balloc is a Go implementation of a buddy memory allocation system. The buddy allocator is an efficient memory management algorithm that divides memory into partitions to satisfy memory requests while minimizing fragmentation and ensuring allocator integrity.

## Features

- Thread-safe memory allocation and deallocation
- Efficient memory coalescing for reduced fragmentation
- Configurable memory pool sizes
- Low overhead memory management

## Installation

```bash
go get github.com/alexlewtschuk/balloc
```

## Usage

```go
import (
    "github.com/alexlewtschuk/balloc/src/balloc"
    "unsafe"
)

// Initialize a memory pool
var pool balloc.BuddyPool
size := uintptr(1 << 20) // 1MB
err := balloc.buddyInit(&pool, size)
if err != nil {
    // Handle error
}

// Allocate memory
ptr, err := balloc.buddyMalloc(&pool, 1024)
if err != nil {
    // Handle allocation error
}

// Use the memory
// ...

// Free memory when done
balloc.buddyFree(&pool, ptr)

// Destroy the pool when no longer needed
err = balloc.buddyDestroy(&pool)
if err != nil {
    // Handle error
}
```

## How It Works

The buddy allocation system works by maintaining blocks of memory that are powers of 2 in size. When a memory request comes in:

1. The allocator finds the smallest block size that can satisfy the request
2. If no suitable block is available, a larger block is split into two "buddies"
3. This process continues until an appropriate sized block is available
4. When memory is freed, the allocator attempts to merge freed blocks with their buddies again to form larger blocks

## Code Reference

### Types

#### `BuddyPool`

The main structure that manages the memory pool.

```go
type BuddyPool struct {
    // Internal implementation
}
```

#### `Avail`

Represents a block in the free list.

```go
type Avail struct {
    // Internal implementation
}
```

### Functions

#### `buddyInit(pool *BuddyPool, size uintptr) error`

Initializes a new buddy memory pool with the specified size.

#### `buddyMalloc(pool *BuddyPool, size uint) (unsafe.Pointer, error)`

Allocates a block of memory of at least the requested size.

#### `buddyFree(pool *BuddyPool, ptr unsafe.Pointer)`

Frees a previously allocated memory block.

#### `buddyDestroy(pool *BuddyPool) error`

Releases all resources associated with the memory pool.

## Constants

- `DEFAULT_K`: Default memory pool size (2^30 bytes)
- `MIN_K`: Minimum memory pool size (2^20 bytes)
- `MAX_K`: Maximum memory pool size (2^48 bytes)
- `SMALLEST_K`: Smallest allocatable block size (2^6 bytes)

## Testing

The project includes comprehensive tests for the buddy allocator functionality:

```bash
cd /path/to/balloc
go test ./src/balloc
```