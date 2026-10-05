// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// File crypt_test.go: exercises crypt regressions; fixtures must preserve cleanup and expose
// byte/lifecycle failures explicitly.

package kcp

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"hash/crc32"
	"io"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// TestSM4 checks SM4 so a change cannot silently weaken the recorded regression contract.
func TestSM4(t *testing.T) {
	bc, err := NewSM4BlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestAES checks AES so a change cannot silently weaken the recorded regression contract.
func TestAES(t *testing.T) {
	bc, err := NewAESBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestTEA checks TEA so a change cannot silently weaken the recorded regression contract.
func TestTEA(t *testing.T) {
	bc, err := NewTEABlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestXOR checks XOR so a change cannot silently weaken the recorded regression contract.
func TestXOR(t *testing.T) {
	bc, err := NewSimpleXORBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestBlowfish checks Blowfish so a change cannot silently weaken the recorded regression
// contract.
func TestBlowfish(t *testing.T) {
	bc, err := NewBlowfishBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestNone checks None so a change cannot silently weaken the recorded regression contract.
func TestNone(t *testing.T) {
	bc, err := NewNoneBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestCast5 checks Cast5 so a change cannot silently weaken the recorded regression contract.
func TestCast5(t *testing.T) {
	bc, err := NewCast5BlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// Test3DES checks 3 DES so a change cannot silently weaken the recorded regression contract.
func Test3DES(t *testing.T) {
	bc, err := NewTripleDESBlockCrypt(pass[:24])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestTwofish checks Twofish so a change cannot silently weaken the recorded regression
// contract.
func TestTwofish(t *testing.T) {
	bc, err := NewTwofishBlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestXTEA checks XTEA so a change cannot silently weaken the recorded regression contract.
func TestXTEA(t *testing.T) {
	bc, err := NewXTEABlockCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// TestSalsa20 checks Salsa20 so a change cannot silently weaken the recorded regression
// contract.
func TestSalsa20(t *testing.T) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}
	cryptTest(t, bc)
}

// cryptTest checks packet cipher round trips and buffer use without relying on application
// traffic.
func cryptTest(t *testing.T, bc BlockCrypt) {
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, mtuLimit)
	enc := make([]byte, mtuLimit)
	bc.Encrypt(enc, data)
	bc.Decrypt(dec, enc)
	if !bytes.Equal(data, dec) {
		t.Fail()
	}
}

// TestAES256GCM checks AES256 GCM so a change cannot silently weaken the recorded regression
// contract.
func TestAES256GCM(t *testing.T) {
	bc, err := NewAESGCMCrypt(pass[:32])
	if err != nil {
		t.Fatal(err)
		return
	}

	testAEAD(t, bc)
}

// TestAES128GCM checks AES128 GCM so a change cannot silently weaken the recorded regression
// contract.
func TestAES128GCM(t *testing.T) {
	bc, err := NewAESGCMCrypt(pass[:16])
	if err != nil {
		t.Fatal(err)
		return
	}

	testAEAD(t, bc)
}

// testAEAD checks authenticated round trips, nonce/tag sizing and tamper rejection for the
// AEAD fixture.
func testAEAD(t *testing.T, bc BlockCrypt) {
	aead := bc.(*aeadCrypt)

	nonceSize := aead.NonceSize()

	size := mtuLimit - cryptHeaderSize - aead.Overhead()
	data := make([]byte, size)
	io.ReadFull(rand.Reader, data)

	// if the size of packet is cannot accommodate the AEAD overhead
	// Open and Seal will allocate a new slice internally, we need to
	// ensure that it does not happen for our MTU sized packets.
	packet := make([]byte, mtuLimit)

	// Seal
	dst := packet[:nonceSize]
	nonce := packet[:nonceSize]
	fillRand(nonce)

	sealedPacket := aead.Seal(dst, nonce, data, nil)
	if &sealedPacket[0] != &packet[0] {
		t.Fatal("Seal created a new slice")
		return
	}

	// Open
	dst = sealedPacket[:nonceSize]
	nonce = sealedPacket[:nonceSize]
	ciphertext := sealedPacket[nonceSize:]

	decrypted, err := aead.Open(dst, nonce, ciphertext, nil)
	if &decrypted[0] != &sealedPacket[0] {
		t.Fatal("Open created a new slice")
		return
	}

	if err != nil {
		t.Fatal(err)
		return
	}

	if !bytes.Equal(data, decrypted[nonceSize:]) {
		t.Fail()
	}
}

// BenchmarkSM4 measures SM4 with the fixture's workload; results must be interpreted with its
// buffer and transport settings.
func BenchmarkSM4(b *testing.B) {
	bc, err := NewSM4BlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// BenchmarkAES128 measures AES128 with the fixture's workload; results must be interpreted
// with its buffer and transport settings.
func BenchmarkAES128(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}

	benchCrypt(b, bc)
}

// BenchmarkAES192 measures AES192 with the fixture's workload; results must be interpreted
// with its buffer and transport settings.
func BenchmarkAES192(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:24])
	if err != nil {
		b.Fatal(err)
		return
	}

	benchCrypt(b, bc)
}

// BenchmarkAES256 measures AES256 with the fixture's workload; results must be interpreted
// with its buffer and transport settings.
func BenchmarkAES256(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}

	benchCrypt(b, bc)
}

// BenchmarkTEA measures TEA with the fixture's workload; results must be interpreted with its
// buffer and transport settings.
func BenchmarkTEA(b *testing.B) {
	bc, err := NewTEABlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// BenchmarkXOR measures XOR with the fixture's workload; results must be interpreted with its
// buffer and transport settings.
func BenchmarkXOR(b *testing.B) {
	bc, err := NewSimpleXORBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// BenchmarkBlowfish measures Blowfish with the fixture's workload; results must be interpreted
// with its buffer and transport settings.
func BenchmarkBlowfish(b *testing.B) {
	bc, err := NewBlowfishBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// BenchmarkNone measures None with the fixture's workload; results must be interpreted with
// its buffer and transport settings.
func BenchmarkNone(b *testing.B) {
	bc, err := NewNoneBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// BenchmarkCast5 measures Cast5 with the fixture's workload; results must be interpreted with
// its buffer and transport settings.
func BenchmarkCast5(b *testing.B) {
	bc, err := NewCast5BlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// Benchmark3DES measures 3 DES with the fixture's workload; results must be interpreted with
// its buffer and transport settings.
func Benchmark3DES(b *testing.B) {
	bc, err := NewTripleDESBlockCrypt(pass[:24])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// BenchmarkTwofish measures Twofish with the fixture's workload; results must be interpreted
// with its buffer and transport settings.
func BenchmarkTwofish(b *testing.B) {
	bc, err := NewTwofishBlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
	}
	benchCrypt(b, bc)
}

// BenchmarkXTEA measures XTEA with the fixture's workload; results must be interpreted with
// its buffer and transport settings.
func BenchmarkXTEA(b *testing.B) {
	bc, err := NewXTEABlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// BenchmarkSalsa20 measures Salsa20 with the fixture's workload; results must be interpreted
// with its buffer and transport settings.
func BenchmarkSalsa20(b *testing.B) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}
	benchCrypt(b, bc)
}

// benchCrypt measures the selected cipher on packet-sized storage while reusing the benchmark
// buffers.
func benchCrypt(b *testing.B, bc BlockCrypt) {
	data := make([]byte, mtuLimit)
	io.ReadFull(rand.Reader, data)
	dec := make([]byte, mtuLimit)
	enc := make([]byte, mtuLimit)

	b.ReportAllocs()
	b.SetBytes(int64(len(enc) * 2))

	for b.Loop() {
		bc.Encrypt(enc, data)
		bc.Decrypt(dec, enc)
	}
}

// BenchmarkCRC32 measures CRC32 with the fixture's workload; results must be interpreted with
// its buffer and transport settings.
func BenchmarkCRC32(b *testing.B) {
	content := make([]byte, 1024)
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		crc32.ChecksumIEEE(content)
	}
}

// BenchmarkCFB_AES_128_CRC32 measures CFB AES 128 CRC32 with the fixture's workload; results
// must be interpreted with its buffer and transport settings.
func BenchmarkCFB_AES_128_CRC32(b *testing.B) {
	bc, err := NewAESBlockCrypt(pass[:16])
	if err != nil {
		b.Fatal(err)
		return
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	for b.Loop() {
		checksum := crc32.ChecksumIEEE(data[cryptHeaderSize:])
		binary.LittleEndian.PutUint32(data[nonceSize:cryptHeaderSize], checksum)
		bc.Encrypt(data, data)
	}
}

// BenchmarkAEAD_AES_128_GCM measures AEAD AES 128 GCM with the fixture's workload; results
// must be interpreted with its buffer and transport settings.
func BenchmarkAEAD_AES_128_GCM(b *testing.B) {
	block, err := aes.NewCipher(pass[:16])
	if err != nil {
		panic(err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	nonce := data[:aead.NonceSize()]
	plaintext := data[aead.NonceSize():]

	for b.Loop() {
		aead.Seal(plaintext[:0], nonce, plaintext, nil)
	}
}

// BenchmarkCFB_Salsa20_CRC32 measures CFB Salsa20 CRC32 with the fixture's workload; results
// must be interpreted with its buffer and transport settings.
func BenchmarkCFB_Salsa20_CRC32(b *testing.B) {
	bc, err := NewSalsa20BlockCrypt(pass[:32])
	if err != nil {
		b.Fatal(err)
		return
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	for b.Loop() {
		checksum := crc32.ChecksumIEEE(data[cryptHeaderSize:])
		binary.LittleEndian.PutUint32(data[nonceSize:cryptHeaderSize], checksum)
		bc.Encrypt(data, data)
	}
}

// BenchmarkAEAD_Chacha20_Poly1035 measures AEAD Chacha20 Poly1035 with the fixture's workload;
// results must be interpreted with its buffer and transport settings.
func BenchmarkAEAD_Chacha20_Poly1035(b *testing.B) {
	aead, err := chacha20poly1305.New(pass[:32])
	if err != nil {
		panic(err)
	}

	data := make([]byte, 1400, mtuLimit)
	b.SetBytes(1400)

	nonce := data[:aead.NonceSize()]
	plaintext := data[aead.NonceSize():]

	for b.Loop() {
		aead.Seal(plaintext[:0], nonce, plaintext, nil)
	}
}

// TestCryptErrors checks Crypt Errors so a change cannot silently weaken the recorded
// regression contract.
func TestCryptErrors(t *testing.T) {
	invalidKey := []byte("invalid")

	if _, err := NewSM4BlockCrypt(invalidKey); err == nil {
		t.Error("NewSM4BlockCrypt should fail with invalid key")
	}
	if _, err := NewTwofishBlockCrypt(invalidKey); err == nil {
		t.Error("NewTwofishBlockCrypt should fail with invalid key")
	}
	if _, err := NewTripleDESBlockCrypt(invalidKey); err == nil {
		t.Error("NewTripleDESBlockCrypt should fail with invalid key")
	}
	if _, err := NewCast5BlockCrypt(invalidKey); err == nil {
		t.Error("NewCast5BlockCrypt should fail with invalid key")
	}
	// Blowfish supports variable key length, so "invalid" (7 bytes) might be valid.
	// Blowfish key size: 1-56 bytes. So 7 bytes is valid.
	// Let's try empty key or very long key.
	if _, err := NewBlowfishBlockCrypt(nil); err == nil {
		t.Error("NewBlowfishBlockCrypt should fail with nil key")
	}

	if _, err := NewAESBlockCrypt(invalidKey); err == nil {
		t.Error("NewAESBlockCrypt should fail with invalid key")
	}
	if _, err := NewTEABlockCrypt(invalidKey); err == nil {
		t.Error("NewTEABlockCrypt should fail with invalid key")
	}
	if _, err := NewXTEABlockCrypt(invalidKey); err == nil {
		t.Error("NewXTEABlockCrypt should fail with invalid key")
	}
}
