package hls

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
)

// MethodAES128 是首期唯一支持的加密方法（RFC 8216 §4.3.2.4）。
const MethodAES128 = "AES-128"

// SequenceIV 按 RFC 8216 §5.2 由媒体序号推导初始向量：序号作为 128 位大端整数左对齐。
// 清单没有给出 EXT-X-KEY 的 IV 时用它。
func SequenceIV(mediaSequence int) []byte {
	iv := make([]byte, 16)
	binary.BigEndian.PutUint64(iv[8:], uint64(mediaSequence))
	return iv
}

// DecryptAES128 按 AES-128-CBC 解密一个分片，并去掉 PKCS7 填充。
// 这是 HLS 标准 AES-128 的完整语义：分片整体加密，填充在最后一个块里。
func DecryptAES128(data, key, iv []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("AES-128 密钥必须是 16 字节，实际 %d 字节", len(key))
	}
	if len(iv) != 16 {
		return nil, fmt.Errorf("AES-128 初始向量必须是 16 字节，实际 %d 字节", len(iv))
	}
	if len(data) == 0 {
		return data, nil
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("加密分片长度 %d 不是 16 的整数倍，无法按 AES-128-CBC 解密", len(data))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)

	// 填充合法就按填充裁剪；不合法时保留原样——有些服务器发出的分片不带 PKCS7 填充，
	// 此时强行裁剪会把真实数据切掉。这里只做「能确认才裁」的保守处理。
	if unpadded, ok := unpadPKCS7(out); ok {
		return unpadded, nil
	}
	return out, nil
}

// unpadPKCS7 校验并去掉 PKCS7 填充，返回是否成功。
func unpadPKCS7(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return data, false
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return data, false
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return data, false
		}
	}
	return data[:len(data)-pad], true
}
