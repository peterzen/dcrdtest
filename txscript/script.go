// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txscript

import (
	"bytes"
	"fmt"
	"strings"
)

// These are the constants specified for maximums in individual scripts.
const (
	MaxOpsPerScript       = 255  // Max number of non-push operations.
	MaxPubKeysPerMultiSig = 20   // Multisig can't have more sigs than this.
	MaxScriptElementSize  = 2048 // Max bytes pushable to the stack.
)

// isSmallInt returns whether or not the opcode is considered a small integer,
// which is an OP_0, or OP_1 through OP_16.
//
// NOTE: This function is only valid for version 0 opcodes.  Since the function
// does not accept a script version, the results are undefined for other script
// versions.
func isSmallInt(op byte) bool {
	return op == OP_0 || (op >= OP_1 && op <= OP_16)
}

// IsPayToScriptHash returns true if the script is in the standard
// pay-to-script-hash (P2SH) format, false otherwise.
func IsPayToScriptHash(script []byte) bool {
	pops, err := parseScript(script)
	if err != nil {
		return false
	}
	return isScriptHash(pops)
}

// isPushOnly returns true if the script only pushes data, false otherwise.
func isPushOnly(pops []parsedOpcode) bool {
	// NOTE: This function does NOT verify opcodes directly since it is
	// internal and is only called with parsed opcodes for scripts that did
	// not have any parse errors.  Thus, consensus is properly maintained.

	for _, pop := range pops {
		// All opcodes up to OP_16 are data push instructions.
		// NOTE: This does consider OP_RESERVED to be a data push
		// instruction, but execution of OP_RESERVED will fail anyways
		// and matches the behavior required by consensus.
		if pop.opcode.value > OP_16 {
			return false
		}
	}
	return true
}

// IsPushOnlyScript returns whether or not the passed script only pushes data.
//
// False will be returned when the script does not parse.
func IsPushOnlyScript(script []byte) bool {
	pops, err := parseScript(script)
	if err != nil {
		return false
	}
	return isPushOnly(pops)
}

// isStakeOpcode returns whether or not the opcode is one of the stake tagging
// opcodes.
func isStakeOpcode(op byte) bool {
	return op >= OP_SSTX && op <= OP_SSTXCHANGE
}

// isScriptHash returns whether or not the passed script is a regular
// pay-to-script-hash script.
func isScriptHash(pops []parsedOpcode) bool {
	return len(pops) == 3 &&
		pops[0].opcode.value == OP_HASH160 &&
		pops[1].opcode.value == OP_DATA_20 &&
		pops[2].opcode.value == OP_EQUAL
}

// isStakeScriptHash returns whether or not the passed script is a stake
// pay-to-script-hash script.
func isStakeScriptHash(pops []parsedOpcode) bool {
	return len(pops) == 4 &&
		isStakeOpcode(pops[0].opcode.value) &&
		pops[1].opcode.value == OP_HASH160 &&
		pops[2].opcode.value == OP_DATA_20 &&
		pops[3].opcode.value == OP_EQUAL
}

// isAnyKindOfScriptHash returns whether or not the passed script is either a
// regular pay-to-script-hash script or a stake pay-to-script-hash script.
func isAnyKindOfScriptHash(pops []parsedOpcode) bool {
	return isScriptHash(pops) || isStakeScriptHash(pops)
}

// HasP2SHScriptSigStakeOpCodes returns an error is the p2sh script has either
// stake opcodes or if the pkscript cannot be retrieved.
//
// DEPRECATED.  This will be removed in the next major version bump.
func HasP2SHScriptSigStakeOpCodes(version uint16, scriptSig, scriptPubKey []byte) error {
	class := GetScriptClass(version, scriptPubKey)
	if IsStakeOutput(scriptPubKey) {
		class, _ = GetStakeOutSubclass(scriptPubKey)
	}
	if class == ScriptHashTy {
		// Obtain the embedded pkScript from the scriptSig of the
		// current transaction. Then, ensure that it does not use
		// any stake tagging OP codes.
		pData, err := PushedData(scriptSig)
		if err != nil {
			return err
		}
		if len(pData) == 0 {
			str := "script has no pushed data"
			return scriptError(ErrNotPushOnly, str)
		}

		// The pay-to-hash-script is the final data push of the
		// signature script.
		shScript := pData[len(pData)-1]

		hasStakeOpCodes, err := ContainsStakeOpCodes(shScript)
		if err != nil {
			return err
		}
		if hasStakeOpCodes {
			str := "stake opcodes were found in a p2sh script"
			return scriptError(ErrP2SHStakeOpCodes, str)
		}
	}

	return nil
}

// parseScriptTemplate is the same as parseScript but allows the passing of the
// template list for testing purposes.  When there are parse errors, it returns
// the list of parsed opcodes up to the point of failure along with the error.
func parseScriptTemplate(script []byte, opcodes *[256]opcode) ([]parsedOpcode, error) {
	retScript := make([]parsedOpcode, 0, len(script))
	for i := 0; i < len(script); {
		instr := script[i]
		op := &opcodes[instr]
		pop := parsedOpcode{opcode: op}

		// Parse data out of instruction.
		switch {
		// No additional data.  Note that some of the opcodes, notably
		// OP_1NEGATE, OP_0, and OP_[1-16] represent the data
		// themselves.
		case op.length == 1:
			i++

		// Data pushes of specific lengths -- OP_DATA_[1-75].
		case op.length > 1:
			if len(script[i:]) < op.length {
				str := fmt.Sprintf("opcode %s requires %d "+
					"bytes, but script only has %d remaining",
					op.name, op.length, len(script[i:]))
				return retScript, scriptError(ErrMalformedPush,
					str)
			}

			// Slice out the data.
			pop.data = script[i+1 : i+op.length]
			i += op.length

		// Data pushes with parsed lengths -- OP_PUSHDATAP{1,2,4}.
		case op.length < 0:
			var l uint
			off := i + 1

			if len(script[off:]) < -op.length {
				str := fmt.Sprintf("opcode %s requires %d "+
					"bytes, but script only has %d remaining",
					op.name, -op.length, len(script[off:]))
				return retScript, scriptError(ErrMalformedPush,
					str)
			}

			// Next -length bytes are little endian length of data.
			switch op.length {
			case -1:
				l = uint(script[off])
			case -2:
				l = ((uint(script[off+1]) << 8) |
					uint(script[off]))
			case -4:
				l = ((uint(script[off+3]) << 24) |
					(uint(script[off+2]) << 16) |
					(uint(script[off+1]) << 8) |
					uint(script[off]))
			default:
				str := fmt.Sprintf("invalid opcode length %d",
					op.length)
				return retScript, scriptError(ErrMalformedPush,
					str)
			}

			// Move offset to beginning of the data.
			off += -op.length

			// Disallow entries that do not fit script or were
			// sign extended.
			if int(l) > len(script[off:]) || int(l) < 0 {
				str := fmt.Sprintf("opcode %s pushes %d bytes, "+
					"but script only has %d remaining",
					op.name, int(l), len(script[off:]))
				return retScript, scriptError(ErrMalformedPush,
					str)
			}

			pop.data = script[off : off+int(l)]
			i += 1 - op.length + int(l)
		}

		retScript = append(retScript, pop)
	}

	return retScript, nil
}

// parseScript preparses the script in bytes into a list of parsedOpcodes while
// applying a number of sanity checks.
func parseScript(script []byte) ([]parsedOpcode, error) {
	return parseScriptTemplate(script, &opcodeArray)
}

// unparseScript reversed the action of parseScript and returns the
// parsedOpcodes as a list of bytes
func unparseScript(pops []parsedOpcode) ([]byte, error) {
	script := make([]byte, 0, len(pops))
	for _, pop := range pops {
		b, err := pop.bytes()
		if err != nil {
			return nil, err
		}
		script = append(script, b...)
	}
	return script, nil
}

// DisasmString formats a disassembled script for one line printing.  When the
// script fails to parse, the returned string will contain the disassembled
// script up to the point the failure occurred along with the string '[error]'
// appended.  In addition, the reason the script failed to parse is returned
// if the caller wants more information about the failure.
//
// NOTE: This function is only valid for version 0 scripts.  Since the function
// does not accept a script version, the results are undefined for other script
// versions.
func DisasmString(script []byte) (string, error) {
	const scriptVersion = 0

	var disbuf strings.Builder
	tokenizer := MakeScriptTokenizer(scriptVersion, script)
	if tokenizer.Next() {
		disasmOpcode(&disbuf, tokenizer.op, tokenizer.Data(), true)
	}
	for tokenizer.Next() {
		disbuf.WriteByte(' ')
		disasmOpcode(&disbuf, tokenizer.op, tokenizer.Data(), true)
	}
	if tokenizer.Err() != nil {
		if tokenizer.ByteIndex() != 0 {
			disbuf.WriteByte(' ')
		}
		disbuf.WriteString("[error]")
	}
	return disbuf.String(), tokenizer.Err()
}

// canonicalPush returns true if the object is either not a push instruction
// or the push instruction contained wherein is matches the canonical form
// or using the smallest instruction to do the job. False otherwise.
func canonicalPush(pop parsedOpcode) bool {
	opcode := pop.opcode.value
	data := pop.data
	dataLen := len(pop.data)
	if opcode > OP_16 {
		return true
	}

	if opcode < OP_PUSHDATA1 && opcode > OP_0 && (dataLen == 1 && data[0] <= 16) {
		return false
	}
	if opcode == OP_PUSHDATA1 && dataLen < OP_PUSHDATA1 {
		return false
	}
	if opcode == OP_PUSHDATA2 && dataLen <= 0xff {
		return false
	}
	if opcode == OP_PUSHDATA4 && dataLen <= 0xffff {
		return false
	}
	return true
}

// removeOpcodeByData will return the script minus any opcodes that would push
// the passed data to the stack.
func removeOpcodeByData(pkscript []parsedOpcode, data []byte) []parsedOpcode {
	retScript := make([]parsedOpcode, 0, len(pkscript))
	for _, pop := range pkscript {
		if !canonicalPush(pop) || !bytes.Contains(pop.data, data) {
			retScript = append(retScript, pop)
		}
	}
	return retScript

}

// asSmallInt returns the passed opcode, which must be true according to
// isSmallInt(), as an integer.
func asSmallInt(op byte) int {
	if op == OP_0 {
		return 0
	}

	return int(op - (OP_1 - 1))
}

// getSigOpCount is the implementation function for counting the number of
// signature operations in the script provided by pops. If precise mode is
// requested then we attempt to count the number of operations for a multisig
// op. Otherwise we use the maximum.
func getSigOpCount(pops []parsedOpcode, precise bool) int {
	nSigs := 0
	for i, pop := range pops {
		switch pop.opcode.value {
		case OP_CHECKSIG:
			fallthrough
		case OP_CHECKSIGVERIFY:
			fallthrough
		case OP_CHECKSIGALT:
			fallthrough
		case OP_CHECKSIGALTVERIFY:
			nSigs++
		case OP_CHECKMULTISIG:
			fallthrough
		case OP_CHECKMULTISIGVERIFY:
			// If we are being precise then look for familiar
			// patterns for multisig, for now all we recognize is
			// OP_1 - OP_16 to signify the number of pubkeys.
			// Otherwise, we use the max of 20.
			if precise && i > 0 &&
				pops[i-1].opcode.value >= OP_1 &&
				pops[i-1].opcode.value <= OP_16 {
				nSigs += asSmallInt(pops[i-1].opcode.value)
			} else {
				nSigs += MaxPubKeysPerMultiSig
			}
		default:
			// Not a sigop.
		}
	}

	return nSigs
}

// GetSigOpCount provides a quick count of the number of signature operations
// in a script. a CHECKSIG operations counts for 1, and a CHECK_MULTISIG for 20.
// If the script fails to parse, then the count up to the point of failure is
// returned.
func GetSigOpCount(script []byte) int {
	// Don't check error since parseScript returns the parsed-up-to-error
	// list of pops.
	pops, _ := parseScript(script)
	return getSigOpCount(pops, false)
}

// GetPreciseSigOpCount returns the number of signature operations in
// scriptPubKey.  If bip16 is true then scriptSig may be searched for the
// Pay-To-Script-Hash script in order to find the precise number of signature
// operations in the transaction.  If the script fails to parse, then the count
// up to the point of failure is returned.
func GetPreciseSigOpCount(scriptSig, scriptPubKey []byte, bip16 bool) int {
	// Don't check error since parseScript returns the parsed-up-to-error
	// list of pops.
	pops, _ := parseScript(scriptPubKey)

	// Treat non P2SH transactions as normal.
	if !(bip16 && isScriptHash(pops)) {
		return getSigOpCount(pops, true)
	}

	// The public key script is a pay-to-script-hash, so parse the signature
	// script to get the final item.  Scripts that fail to fully parse count
	// as 0 signature operations.
	sigPops, err := parseScript(scriptSig)
	if err != nil {
		return 0
	}

	// The signature script must only push data to the stack for P2SH to be
	// a valid pair, so the signature operation count is 0 when that is not
	// the case.
	if !isPushOnly(sigPops) || len(sigPops) == 0 {
		return 0
	}

	// The P2SH script is the last item the signature script pushes to the
	// stack.  When the script is empty, there are no signature operations.
	shScript := sigPops[len(sigPops)-1].data
	if len(shScript) == 0 {
		return 0
	}

	// Parse the P2SH script and don't check the error since parseScript
	// returns the parsed-up-to-error list of pops and the consensus rules
	// dictate signature operations are counted up to the first parse
	// failure.
	shPops, _ := parseScript(shScript)
	return getSigOpCount(shPops, true)
}

// checkScriptParses returns an error if the provided script fails to parse.
func checkScriptParses(scriptVersion uint16, script []byte) error {
	tokenizer := MakeScriptTokenizer(scriptVersion, script)
	for tokenizer.Next() {
		// Nothing to do.
	}
	return tokenizer.Err()
}

// IsUnspendable returns whether the passed public key script is unspendable, or
// guaranteed to fail at execution.  This allows inputs to be pruned instantly
// when entering the UTXO set. In Decred, all zero value outputs are unspendable.
func IsUnspendable(amount int64, pkScript []byte) bool {
	if amount == 0 {
		return true
	}

	pops, err := parseScript(pkScript)
	if err != nil {
		return true
	}

	return len(pops) > 0 && pops[0].opcode.value == OP_RETURN
}
