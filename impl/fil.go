package impl

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	crypto2 "github.com/ethereum/go-ethereum/crypto"
	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
	"strings"
	//logging "github.com/ipfs/go-log/v2"
)

const filPath = "m/44'/461'/0'/0/"

// PrivateKeyBytes is the size of a serialized private key.
const PrivateKeyBytes = 32

// PublicKeyBytes is the size of a serialized public key.
const PublicKeyBytes = 65

type SecretKey = ffi.PrivateKey

func CreateSecp256k1FilAddress(mnemonic string, userId int) (string, error) {

	priKey, err := generateSecp256k1PriviteKey(mnemonic, userId)
	if err != nil {
		fmt.Printf("private key get err:%+v", err)
		return "", err
	}

	secpAddr, err := generateSecp256addr(priKey)
	if err != nil {
		fmt.Printf("call secp256addr to get addr err:%+v", err)
		return "", err
	}

	return secpAddr, nil
}

func CreateBlsFilAddress(mnemonic string, userId int) (string, error) {

	priKey, err := generateBLSPriviteKey(mnemonic, userId)
	if err != nil {
		fmt.Printf("private key get err:%+v", err)
		return "", err
	}

	addr, err := generateBlsAddr(priKey[:])
	if err != nil {
		fmt.Printf("call blsAddr to get addr err:%+v", err)
		return "", err
	}

	return addr, nil
}
func generateSecp256k1PriviteKey(mnemonic string, userId int) (*ecdsa.PrivateKey, error) {
	dPath := fmt.Sprintf("%s%d", filPath, userId)

	priKey, err := getPrivateKey(mnemonic, dPath)
	if err != nil {
		fmt.Printf("private key get err:%+v", err)
		return nil, err
	}
	return priKey, err
}

func generateBLSPriviteKey(mnemonic string, userId int) ([32]byte, error) {
	dPath := fmt.Sprintf("%s%d", filPath, userId)

	priKey, err := getPrivateKeyBytes(mnemonic, dPath)
	if err != nil {
		fmt.Printf("private key get err:%+v", err)
		return [32]byte{}, err
	}

	var ikm [32]byte
	copy(ikm[:], priKey[:32])
	//fmt.Println("len(ikm) = ", len(ikm), " ikm = ", ikm)
	//fmt.Println("len(priKey) = ", len(priKey), " prikey = ", priKey)
	sk := ffi.PrivateKeyGenerateWithSeed(ikm)
	return sk, err
}

func ExportSecp256k1Address(mnemonic string, userId int) (string, error) {

	priKey, err := generateSecp256k1PriviteKey(mnemonic, userId)
	if err != nil {
		fmt.Printf("private key get err:%+v", err)
		return "", err
	}

	return exportWallet(priKey)
}

func VerifyPassword(mnemonic string, userId int)bool  {
	dPath := fmt.Sprintf("%s%d", filPath, userId)

	_, err := getPrivateKeyBytes(mnemonic, dPath)
	if err != nil {
		fmt.Printf("private key get err:%+v", err)
		return false
	}

	return true

}

func exportWallet(priKey *ecdsa.PrivateKey) (string, error) {
	privkey := make([]byte, PrivateKeyBytes)
	blob := priKey.D.Bytes()

	// the length is guaranteed to be fixed, given the serialization rules for secp2561k curve points.
	copy(privkey[PrivateKeyBytes-len(blob):], blob)

	b, err := json.Marshal(types.KeyInfo{Type: types.KTSecp256k1, PrivateKey: privkey})
	if err != nil {
		return "", err
	}

	//fmt.Println(hex.EncodeToString(b))
	return hex.EncodeToString(b), nil
}

func ExportBlsAddress(mnemonic string, userId int) (string, error) {

	privkey, err := generateBLSPriviteKey(mnemonic, userId)
	if err != nil {
		fmt.Printf("private key get err:%+v", err)
		return "", err
	}

	b, err := json.Marshal(types.KeyInfo{Type: types.KTBLS, PrivateKey: privkey[:]})
	if err != nil {
		return "", err
	}

	//fmt.Println(hex.EncodeToString(b))
	return hex.EncodeToString(b), nil
}

func generateSecp256addr(priKey *ecdsa.PrivateKey) (string, error) {
	ret := make([]byte, 1+2*32)
	ret[0] = 4
	priKey.PublicKey.X.FillBytes(ret[1 : 1+32])
	priKey.PublicKey.Y.FillBytes(ret[1+32:])
	//fmt.Println(ret)
	secpAddr, err := address.NewSecp256k1Address(ret)
	if err != nil {
		return "", err
	}
	return secpAddr.String(), nil
}

func generateBlsAddr(priv []byte) (string, error) {
	if priv == nil || len(priv) != ffi.PrivateKeyBytes {
		return "", fmt.Errorf("bls signature invalid private key")
	}

	sk := new([32]byte)
	copy(sk[:], priv[:ffi.PrivateKeyBytes])

	pubkey := ffi.PrivateKeyPublicKey(*sk)

	//return pubkey[:], nil

	blsaddr, err := address.NewBLSAddress(pubkey[:])
	if err != nil {
		return "", err
	}
	return blsaddr.String(), nil
}

func SignMessage(msg *types.Message, mnenoic string, index int) (*types.SignedMessage, error) {
	mb, err := msg.ToStorageBlock()
	if err != nil {
		fmt.Printf("序列化消息失败， err:%v", err)
		return &types.SignedMessage{}, xerrors.Errorf("serializing message: %w", err)
	}

	var sb *crypto.Signature
	if strings.HasPrefix(msg.From.String(), "f3") || strings.HasPrefix(msg.From.String(), "t3") {
		privKey, err := generateBLSPriviteKey(mnenoic, index)
		if err != nil {
			fmt.Printf("private key get err:%+v", err)
			return &types.SignedMessage{}, err
		}

		//sb, err = sigs.Sign(crypto.SigTypeBLS, privKey[:], mb.Cid().Bytes())
		sb, err = signBls(privKey[:], mb.Cid().Bytes())
		if err != nil {
			fmt.Printf("签名消息失败，err:%v", err)
			return &types.SignedMessage{}, err
		}
	} else {
		priKey, err := generateSecp256k1PriviteKey(mnenoic, index)
		if err != nil {
			fmt.Printf("private key get err:%+v", err)
			return &types.SignedMessage{}, err
		}

		sig, err := crypto2.Sign(mb.Cid().Bytes(), priKey)
		if err != nil {
			fmt.Printf("签名消息失败，err:%v", err)
			return &types.SignedMessage{}, err
		}

		sb = &crypto.Signature{
			Type: crypto.SigTypeBLS,
			Data: sig,
		}
	}

	return &types.SignedMessage{
		Message:   *msg,
		Signature: *sb,
	}, nil

}

func signBls(p []byte, msg []byte) (*crypto.Signature, error) {
	if p == nil || len(p) != ffi.PrivateKeyBytes {
		return nil, fmt.Errorf("bls signature invalid private key")
	}

	sk := new(SecretKey)
	copy(sk[:], p[:ffi.PrivateKeyBytes])

	sig := ffi.PrivateKeySign(*sk, msg)

	return &crypto.Signature{
		Type: crypto.SigTypeBLS,
		Data: sig[:],
	}, nil
}
