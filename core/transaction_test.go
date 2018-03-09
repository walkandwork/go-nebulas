// Copyright (C) 2017 go-nebulas authors
//
// This file is part of the go-nebulas library.
//
// the go-nebulas library is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// the go-nebulas library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with the go-nebulas library.  If not, see <http://www.gnu.org/licenses/>.
//

package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/nebulasio/go-nebulas/core/pb"
	"github.com/nebulasio/go-nebulas/core/state"
	"github.com/nebulasio/go-nebulas/crypto"
	"github.com/nebulasio/go-nebulas/crypto/keystore"
	"github.com/nebulasio/go-nebulas/util"
	"github.com/nebulasio/go-nebulas/util/byteutils"
	"github.com/stretchr/testify/assert"
)

func mockNormalTransaction(chainID uint32, nonce uint64) *Transaction {
	// payload, _ := NewBinaryPayload(nil).ToBytes()
	return mockTransaction(chainID, nonce, TxPayloadBinaryType, nil)
}

func mockDeployTransaction(chainID uint32, nonce uint64) *Transaction {
	source := `
	"use strict";var StandardToken=function(){LocalContractStorage.defineProperties(this,{name:null,symbol:null,_totalSupply:null,totalIssued:null});LocalContractStorage.defineMapProperty(this,"balances")};StandardToken.prototype={init:function(name,symbol,totalSupply){this.name=name;this.symbol=symbol;this._totalSupply=totalSupply;this.totalIssued=0},totalSupply:function(){return this._totalSupply},balanceOf:function(owner){return this.balances.get(owner)||0},transfer:function(to,value){var balance=this.balanceOf(msg.sender);if(balance<value){return false}var finalBalance=balance-value;this.balances.set(msg.sender,finalBalance);this.balances.set(to,this.balanceOf(to)+value);return true},pay:function(msg,amount){if(this.totalIssued+amount>this._totalSupply){throw new Error("too much amount, exceed totalSupply")}this.balances.set(msg.sender,this.balanceOf(msg.sender)+amount);this.totalIssued+=amount}};module.exports=StandardToken;
	`
	sourceType := "js"
	args := `["NebulasToken", "NAS", 1000000000]`
	payload, _ := NewDeployPayload(source, sourceType, args).ToBytes()
	return mockTransaction(chainID, nonce, TxPayloadDeployType, payload)
}

func mockCallTransaction(chainID uint32, nonce uint64, function, args string) *Transaction {
	payload, _ := NewCallPayload(function, args).ToBytes()
	return mockTransaction(chainID, nonce, TxPayloadCallType, payload)
}

func mockTransaction(chainID uint32, nonce uint64, payloadType string, payload []byte) *Transaction {
	from := mockAddress()
	to := mockAddress()
	tx := NewTransaction(chainID, from, to, util.NewUint128(), nonce, payloadType, payload, TransactionGasPrice, TransactionMaxGas)
	return tx
}

func TestTransaction(t *testing.T) {
	type fields struct {
		hash      byteutils.Hash
		from      *Address
		to        *Address
		value     *util.Uint128
		nonce     uint64
		timestamp int64
		alg       uint8
		data      *corepb.Data
		gasPrice  *util.Uint128
		gasLimit  *util.Uint128
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			"full struct",
			fields(fields{
				[]byte("123455"),
				mockAddress(),
				mockAddress(),
				util.NewUint128(),
				456,
				time.Now().Unix(),
				12,
				&corepb.Data{Type: TxPayloadBinaryType, Payload: []byte("hwllo")},
				util.NewUint128(),
				util.NewUint128(),
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &Transaction{
				hash:      tt.fields.hash,
				from:      tt.fields.from,
				to:        tt.fields.to,
				value:     tt.fields.value,
				nonce:     tt.fields.nonce,
				timestamp: tt.fields.timestamp,
				alg:       tt.fields.alg,
				data:      tt.fields.data,
				gasPrice:  tt.fields.gasPrice,
				gasLimit:  tt.fields.gasLimit,
			}
			msg, _ := tx.ToProto()
			ir, _ := proto.Marshal(msg)
			ntx := new(Transaction)
			nMsg := new(corepb.Transaction)
			proto.Unmarshal(ir, nMsg)
			ntx.FromProto(nMsg)
			ntx.timestamp = tx.timestamp
			if !reflect.DeepEqual(tx, ntx) {
				t.Errorf("Transaction.Serialize() = %v, want %v", *tx, *ntx)
			}
		})
	}
}

func TestTransaction_VerifyIntegrity(t *testing.T) {
	testCount := 3
	type testTx struct {
		name   string
		tx     *Transaction
		signer keystore.Signature
		count  int
	}

	tests := []testTx{}
	ks := keystore.DefaultKS

	for index := 0; index < testCount; index++ {

		from := mockAddress()
		to := mockAddress()

		key1, _ := ks.GetUnlocked(from.String())
		signature, _ := crypto.NewSignature(keystore.SECP256K1)
		signature.InitSign(key1.(keystore.PrivateKey))

		gasLimit, _ := util.NewUint128FromInt(200000)
		tx := NewTransaction(1, from, to, util.NewUint128(), 10, TxPayloadBinaryType, []byte("datadata"), TransactionGasPrice, gasLimit)

		test := testTx{string(index), tx, signature, 1}
		tests = append(tests, test)
	}
	for _, tt := range tests {
		for index := 0; index < tt.count; index++ {
			t.Run(tt.name, func(t *testing.T) {
				err := tt.tx.Sign(tt.signer)
				if err != nil {
					t.Errorf("Sign() error = %v", err)
					return
				}
				err = tt.tx.VerifyIntegrity(tt.tx.chainID)
				if err != nil {
					t.Errorf("verify failed:%s", err)
					return
				}
			})
		}
	}
}

func TestTransaction_VerifyExecution(t *testing.T) {
	type testTx struct {
		name            string
		tx              *Transaction
		fromBalance     *util.Uint128
		gasUsed         *util.Uint128
		wanted          error
		afterBalance    *util.Uint128
		toBalance       *util.Uint128
		coinbaseBalance *util.Uint128
		eventTopic      []string
	}
	tests := []testTx{}

	bc := testNeb(t).chain

	// 1NAS = 10^18
	balance, _ := util.NewUint128FromString("1000000000000000000")
	// normal tx
	normalTx := mockNormalTransaction(bc.chainID, 0)
	normalTx.value, _ = util.NewUint128FromInt(1000000)
	gasConsume, err := normalTx.gasPrice.Mul(MinGasCountPerTransaction)
	assert.Nil(t, err)
	afterBalance, err := balance.Sub(gasConsume)
	assert.Nil(t, err)
	afterBalance, err = afterBalance.Sub(normalTx.value)
	coinbaseBalance, err := normalTx.gasPrice.Mul(MinGasCountPerTransaction)
	assert.Nil(t, err)
	tests = append(tests, testTx{
		name:            "normal tx",
		tx:              normalTx,
		fromBalance:     balance,
		gasUsed:         MinGasCountPerTransaction,
		afterBalance:    afterBalance,
		toBalance:       normalTx.value,
		coinbaseBalance: coinbaseBalance,
		wanted:          nil,
		eventTopic:      []string{TopicExecuteTxSuccess},
	})

	// contract deploy tx
	deployTx := mockDeployTransaction(bc.chainID, 0)
	deployTx.value = util.NewUint128()
	gasUsed, _ := util.NewUint128FromInt(21232)
	coinbaseBalance, err = deployTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	balanceConsume, err := deployTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	afterBalance, err = balance.Sub(balanceConsume)
	assert.Nil(t, err)
	tests = append(tests, testTx{
		name:            "contract deploy tx",
		tx:              deployTx,
		fromBalance:     balance,
		gasUsed:         gasUsed,
		afterBalance:    afterBalance,
		toBalance:       deployTx.value,
		coinbaseBalance: coinbaseBalance,
		wanted:          nil,
		eventTopic:      []string{TopicExecuteTxSuccess},
	})

	// contract call tx
	callTx := mockCallTransaction(bc.chainID, 1, "totalSupply", "")
	callTx.value = util.NewUint128()
	gasUsed, _ = util.NewUint128FromInt(20036)
	coinbaseBalance, err = callTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	balanceConsume, err = callTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	afterBalance, err = balance.Sub(balanceConsume)

	tests = append(tests, testTx{
		name:            "contract call tx",
		tx:              callTx,
		fromBalance:     balance,
		gasUsed:         gasUsed,
		afterBalance:    afterBalance,
		toBalance:       callTx.value,
		coinbaseBalance: coinbaseBalance,
		wanted:          nil,
		eventTopic:      []string{TopicExecuteTxFailed},
	})

	// normal tx insufficient fromBalance before execution
	insufficientBlanceTx := mockNormalTransaction(bc.chainID, 0)
	insufficientBlanceTx.value = util.NewUint128()
	tests = append(tests, testTx{
		name:         "normal tx insufficient fromBalance",
		tx:           insufficientBlanceTx,
		fromBalance:  util.NewUint128(),
		gasUsed:      util.NewUint128(),
		afterBalance: util.NewUint128(),
		toBalance:    insufficientBlanceTx.value,
		wanted:       ErrInsufficientBalance,
		eventTopic:   []string{TopicExecuteTxFailed},
	})

	// normal tx out of  gasLimit
	outOfGasLimitTx := mockNormalTransaction(bc.chainID, 0)
	outOfGasLimitTx.value = util.NewUint128()
	outOfGasLimitTx.gasLimit, _ = util.NewUint128FromInt(1)
	tests = append(tests, testTx{
		name:         "normal tx out of gasLimit",
		tx:           outOfGasLimitTx,
		fromBalance:  balance,
		gasUsed:      util.NewUint128(),
		afterBalance: balance,
		toBalance:    util.NewUint128(),
		wanted:       ErrOutOfGasLimit,
		eventTopic:   []string{TopicExecuteTxFailed},
	})

	// tx payload load err
	payloadErrTx := mockDeployTransaction(bc.chainID, 0)
	payloadErrTx.value = util.NewUint128()
	payloadErrTx.data.Payload = []byte("0x00")
	gasCountOfTxBase, err := payloadErrTx.GasCountOfTxBase()
	assert.Nil(t, err)
	coinbaseBalance, err = payloadErrTx.gasPrice.Mul(gasCountOfTxBase)
	assert.Nil(t, err)
	balanceConsume, err = payloadErrTx.gasPrice.Mul(gasCountOfTxBase)
	assert.Nil(t, err)
	afterBalance, err = balance.Sub(balanceConsume)
	assert.Nil(t, err)
	getUsed, err := payloadErrTx.GasCountOfTxBase()
	assert.Nil(t, err)
	tests = append(tests, testTx{
		name:            "payload error tx",
		tx:              payloadErrTx,
		fromBalance:     balance,
		gasUsed:         getUsed,
		afterBalance:    afterBalance,
		toBalance:       util.NewUint128(),
		coinbaseBalance: coinbaseBalance,
		wanted:          nil,
		eventTopic:      []string{TopicExecuteTxFailed},
	})

	// tx execution err
	executionErrTx := mockCallTransaction(bc.chainID, 0, "test", "")
	executionErrTx.value = util.NewUint128()
	gasUsed, _ = util.NewUint128FromInt(20029)
	coinbaseBalance, _ = executionErrTx.gasPrice.Mul(gasUsed)
	balanceConsume, err = executionErrTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	afterBalance, err = balance.Sub(balanceConsume)
	assert.Nil(t, err)
	tests = append(tests, testTx{
		name:            "execution err tx",
		tx:              executionErrTx,
		fromBalance:     balance,
		gasUsed:         gasUsed,
		afterBalance:    afterBalance,
		toBalance:       util.NewUint128(),
		coinbaseBalance: coinbaseBalance,
		wanted:          nil,
		eventTopic:      []string{TopicExecuteTxFailed},
	})

	// tx execution insufficient fromBalance after execution
	executionInsufficientBalanceTx := mockDeployTransaction(bc.chainID, 0)
	executionInsufficientBalanceTx.value = balance
	gasUsed, _ = util.NewUint128FromInt(21232)
	coinbaseBalance, err = executionInsufficientBalanceTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	balanceConsume, err = normalTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	afterBalance, err = balance.Sub(balanceConsume)
	assert.Nil(t, err)
	tests = append(tests, testTx{
		name:            "execution insufficient fromBalance after execution tx",
		tx:              executionInsufficientBalanceTx,
		fromBalance:     balance,
		gasUsed:         gasUsed,
		afterBalance:    afterBalance,
		toBalance:       util.NewUint128(),
		coinbaseBalance: coinbaseBalance,
		wanted:          nil,
		eventTopic:      []string{TopicExecuteTxFailed},
	})

	// tx execution equal fromBalance after execution
	executionEqualBalanceTx := mockDeployTransaction(bc.chainID, 0)
	gasUsed, _ = util.NewUint128FromInt(21232)
	coinbaseBalance, err = executionInsufficientBalanceTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	executionEqualBalanceTx.value = balance
	gasCost, err := executionEqualBalanceTx.gasPrice.Mul(gasUsed)
	assert.Nil(t, err)
	fromBalance, err := gasCost.Add(balance)
	assert.Nil(t, err)
	tests = append(tests, testTx{
		name:            "execution equal fromBalance after execution tx",
		tx:              executionEqualBalanceTx,
		fromBalance:     fromBalance,
		gasUsed:         gasUsed,
		afterBalance:    util.NewUint128(),
		toBalance:       balance,
		coinbaseBalance: coinbaseBalance,
		wanted:          nil,
		eventTopic:      []string{TopicExecuteTxSuccess},
	})

	ks := keystore.DefaultKS
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, _ := ks.GetUnlocked(tt.tx.from.String())
			signature, _ := crypto.NewSignature(keystore.SECP256K1)
			signature.InitSign(key.(keystore.PrivateKey))

			err := tt.tx.Sign(signature)
			assert.Nil(t, err)

			block := bc.tailBlock
			block.begin()
			fromAcc, err := block.accState.GetOrCreateUserAccount(tt.tx.from.address)
			assert.Nil(t, err)
			fromAcc.AddBalance(tt.fromBalance)

			gasUsed, executionErr := tt.tx.VerifyExecution(block)

			fromAcc, err = block.accState.GetOrCreateUserAccount(tt.tx.from.address)
			assert.Nil(t, err)
			toAcc, err := block.accState.GetOrCreateUserAccount(tt.tx.to.address)
			assert.Nil(t, err)
			coinbaseAcc, err := block.accState.GetOrCreateUserAccount(block.header.coinbase.address)
			assert.Nil(t, err)
			if tt.gasUsed != nil {
				assert.Equal(t, tt.gasUsed, gasUsed)
			}
			if tt.afterBalance != nil {
				assert.Equal(t, tt.afterBalance.String(), fromAcc.Balance().String())
			}
			if tt.toBalance != nil {
				assert.Equal(t, tt.toBalance, toAcc.Balance())
			}
			if tt.coinbaseBalance != nil {
				assert.Equal(t, tt.coinbaseBalance, coinbaseAcc.Balance())
			}

			assert.Equal(t, tt.wanted, executionErr)

			events, _ := block.FetchEvents(tt.tx.hash)

			for index, event := range events {
				assert.Equal(t, tt.eventTopic[index], event.Topic)
			}

			block.rollback()
		})
	}

}

func TestTransaction_LocalExecution(t *testing.T) {
	type testCase struct {
		name    string
		tx      *Transaction
		gasUsed *util.Uint128
		result  string
		wanted  error
	}

	tests := []testCase{}

	bc := testNeb(t).chain

	normalTx := mockNormalTransaction(bc.chainID, 0)
	normalTx.value, _ = util.NewUint128FromInt(1000000)
	tests = append(tests, testCase{
		name:    "normal tx",
		tx:      normalTx,
		gasUsed: MinGasCountPerTransaction,
		result:  "",
		wanted:  nil,
	})

	deployTx := mockDeployTransaction(bc.chainID, 0)
	deployTx.value = util.NewUint128()
	gasUsed, _ := util.NewUint128FromInt(21232)
	tests = append(tests, testCase{
		name:    "contract deploy tx",
		tx:      deployTx,
		gasUsed: gasUsed,
		result:  "undefined",
		wanted:  nil,
	})

	// contract call tx
	callTx := mockCallTransaction(bc.chainID, 1, "totalSupply", "")
	callTx.value = util.NewUint128()
	gasUsed, _ = util.NewUint128FromInt(20036)
	tests = append(tests, testCase{
		name:    "contract call tx",
		tx:      callTx,
		gasUsed: gasUsed,
		result:  "",
		wanted:  state.ErrAccountNotFound,
	})

	block := bc.tailBlock

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			fromAcc, err := block.accState.GetOrCreateUserAccount(tt.tx.from.address)
			assert.Nil(t, err)
			fromBefore := fromAcc.Balance()

			toAcc, err := block.accState.GetOrCreateUserAccount(tt.tx.to.address)
			assert.Nil(t, err)
			toBefore := toAcc.Balance()

			coinbaseAcc, err := block.accState.GetOrCreateUserAccount(block.header.coinbase.address)
			assert.Nil(t, err)
			coinbaseBefore := coinbaseAcc.Balance()

			gasUsed, result, err := tt.tx.LocalExecution(block)

			assert.Equal(t, tt.wanted, err)
			assert.Equal(t, tt.result, result)
			assert.Equal(t, tt.gasUsed, gasUsed)

			fromAcc, err = block.accState.GetOrCreateUserAccount(tt.tx.from.address)
			assert.Nil(t, err)
			assert.Equal(t, fromBefore, fromAcc.Balance())

			toAcc, err = block.accState.GetOrCreateUserAccount(tt.tx.to.address)
			assert.Nil(t, err)
			assert.Equal(t, toBefore, toAcc.Balance())

			coinbaseAcc, err = block.accState.GetOrCreateUserAccount(block.header.coinbase.address)
			assert.Nil(t, err)
			assert.Equal(t, coinbaseBefore, coinbaseAcc.Balance())
		})
	}
}
