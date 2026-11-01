package vectors

import (
	"crypto/aes"
	"encoding/binary"
	"errors"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
)

var errSingular = errors.New("vectors: the GHASH system has no solution")

// fieldElement is an element of GF(2^128) in the bit order of GCM (NIST SP 800-38D).
type fieldElement struct{ high, low uint64 }

func elementOf(block []byte) fieldElement {
	return fieldElement{binary.BigEndian.Uint64(block[:8]), binary.BigEndian.Uint64(block[8:16])}
}

func (a fieldElement) bytes() []byte {
	return binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64(nil, a.high), a.low)
}

func (a fieldElement) add(b fieldElement) fieldElement {
	return fieldElement{a.high ^ b.high, a.low ^ b.low}
}

func (a fieldElement) multiply(b fieldElement) fieldElement {
	var product fieldElement
	shifted := b
	for i := range 128 {
		var bit uint64
		if i < 64 {
			bit = (a.high >> (63 - i)) & 1
		} else {
			bit = (a.low >> (127 - i)) & 1
		}
		if bit == 1 {
			product = product.add(shifted)
		}
		carry := shifted.low & 1
		shifted.low = shifted.low>>1 | shifted.high<<63
		shifted.high >>= 1
		if carry == 1 {
			shifted.high ^= 0xe1 << 56
		}
	}
	return product
}

func (a fieldElement) power(exponent uint64) fieldElement {
	result := fieldElement{high: 1 << 63}
	for base := a; exponent > 0; exponent >>= 1 {
		if exponent&1 == 1 {
			result = result.multiply(base)
		}
		base = base.multiply(base)
	}
	return result
}

func (a fieldElement) inverse() fieldElement {
	result := fieldElement{high: 1 << 63}
	square := a
	for range 127 {
		square = square.multiply(square)
		result = result.multiply(square)
	}
	return result
}

func ghash(hashKey fieldElement, aad, ciphertext []byte) fieldElement {
	var state fieldElement
	absorb := func(data []byte) {
		for i := 0; i < len(data); i += 16 {
			block := make([]byte, 16)
			copy(block, data[i:min(i+16, len(data))])
			state = state.add(elementOf(block)).multiply(hashKey)
		}
	}
	absorb(aad)
	absorb(ciphertext)
	lengths := binary.BigEndian.AppendUint64(nil, uint64(len(aad))*8)
	lengths = binary.BigEndian.AppendUint64(lengths, uint64(len(ciphertext))*8)
	return state.add(elementOf(lengths)).multiply(hashKey)
}

// twoKeyObject builds an object whose AES-GCM tag verifies under the content keys of two shares
// (Len, Grubbs and Ristenpart 2021); its commitment comes from the first, so only the first passes it.
func twoKeyObject(
	id core.ID,
	secret *[core.SecretLen]byte,
	shares [2]*[core.ShareLen]byte,
	header envelope.Header,
	policy envelope.Policy,
) ([]byte, error) {
	var contents [2]keys.ContentKeys
	for i, share := range shares {
		derived, err := keys.DeriveAll(id, secret, core.KDFNone, "", share)
		if err != nil {
			return nil, err
		}
		contents[i] = derived.Content
	}
	commitment := envelope.Commitment(&contents[0].Commitment, header, id, policy)
	aad := envelope.AAD(header, commitment[:], id, policy)
	ciphertext := deterministic("two-key/ciphertext", envelope.Pad-envelope.PrefixLen-envelope.TagLen)
	freeBlocks := [2]int{100, 101}
	for _, block := range freeBlocks {
		clear(ciphertext[block*16 : block*16+16])
	}
	tag := deterministic("two-key/tag", envelope.TagLen)
	aadBlocks := (len(aad) + 15) / 16
	totalBlocks := aadBlocks + (len(ciphertext)+15)/16 + 1
	var matrix [2][2]fieldElement
	var targets [2]fieldElement
	for i := range contents {
		block, err := aes.NewCipher(contents[i].K[:])
		if err != nil {
			return nil, err
		}
		hashBlock := make([]byte, 16)
		block.Encrypt(hashBlock, hashBlock)
		hashKey := elementOf(hashBlock)
		counter := make([]byte, 16)
		copy(counter, header.IV[:])
		counter[15] = 1
		mask := make([]byte, 16)
		block.Encrypt(mask, counter)
		targets[i] = elementOf(tag).add(elementOf(mask)).add(ghash(hashKey, aad, ciphertext))
		for j, free := range freeBlocks {
			matrix[i][j] = hashKey.power(exponent(totalBlocks - aadBlocks - free))
		}
	}
	solution, err := solve(matrix, targets)
	if err != nil {
		return nil, err
	}
	for j, free := range freeBlocks {
		copy(ciphertext[free*16:], solution[j].bytes())
	}
	object := append(header.Bytes(), commitment[:]...)
	object = append(object, ciphertext...)
	return append(object, tag...), nil
}

func solve(matrix [2][2]fieldElement, targets [2]fieldElement) ([2]fieldElement, error) {
	determinant := matrix[0][0].multiply(matrix[1][1]).add(matrix[0][1].multiply(matrix[1][0]))
	if determinant == (fieldElement{}) {
		return [2]fieldElement{}, errSingular
	}
	inverse := determinant.inverse()
	first := targets[0].multiply(matrix[1][1]).add(matrix[0][1].multiply(targets[1])).multiply(inverse)
	second := matrix[0][0].multiply(targets[1]).add(targets[0].multiply(matrix[1][0])).multiply(inverse)
	return [2]fieldElement{first, second}, nil
}

func exponent(blocksAfter int) uint64 {
	if blocksAfter < 0 {
		panic("vectors: a free block lies after the end of the ciphertext")
	}
	return uint64(blocksAfter)
}
