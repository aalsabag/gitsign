//
// Copyright 2022 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package root

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sigstore/gitsign/internal/commands/verify"
	"github.com/sigstore/gitsign/internal/gitsign"
	"github.com/sigstore/gitsign/internal/gpg"
	gsio "github.com/sigstore/gitsign/internal/io"
)

// commandSign implements gitsign commit verification.
// This is implemented as a root command so that user can specify the
// gitsign binary directly in their gitconfigs.
func commandVerify(o *options, s *gsio.Streams, args ...string) error {
	ctx := context.Background()

	// Flag validation
	if o.FlagSign {
		return errors.New("specify --help, --sign, or --verify")
	}
	if o.FlagDetachedSignature {
		return errors.New("detach-sign cannot be specified for verification")
	}
	if o.FlagArmor {
		return errors.New("armor cannot be specified for verification")
	}

	gpgout := gpg.NewStatusWriterFromFD(uintptr(o.FlagStatusFD))
	gpgout.Emit(gpg.StatusNewSig)

	var (
		data, sig []byte
		err       error
	)
	detached := len(args) >= 2
	if detached {
		data, sig, err = readDetached(s, args...)
	} else {
		sig, err = readAttached(s, args...)
	}
	if err != nil {
		return fmt.Errorf("failed to read signature data (detached: %T): %w", detached, err)
	}

	v, err := gitsign.NewVerifierWithCosignOpts(ctx, o.Config, nil)
	if err != nil {
		return err
	}
	summary, err := v.Verify(ctx, data, sig, true)
	if err != nil {
		return err
	}

	if err != nil {
		if summary != nil && summary.Cert != nil {
			gpgout.EmitBadSig(summary.Cert)
		} else {
			// TODO: We're omitting a bunch of arguments here.
			gpgout.Emit(gpg.StatusErrSig)
		}
		return fmt.Errorf("failed to verify signature: %w", err)
	}

	verify.PrintSummary(s.Err, summary)
	fmt.Fprintln(s.Err, "WARNING: git verify-commit does not verify cert claims. Prefer using `gitsign verify` instead.")

	gpgout.EmitGoodSig(summary.Cert)
	gpgout.EmitTrustFully()

	return nil
}

func readAttached(s *gsio.Streams, args ...string) ([]byte, error) {
	var (
		f   io.Reader
		err error
	)

	// Read in signature
	if len(args) == 1 {
		f2, err := os.Open(args[0])
		if err != nil {
			return nil, fmt.Errorf("failed to open signature file (%s): %w", args[0], err)
		}
		defer f2.Close()
		f = f2
	} else {
		f = s.In
	}

	sig := new(bytes.Buffer)
	if _, err = io.Copy(sig, f); err != nil {
		return nil, fmt.Errorf("failed to read signature: %w", err)
	}

	return sig.Bytes(), nil
}

func readDetached(s *gsio.Streams, args ...string) ([]byte, []byte, error) {
	// Read in signature
	sigFile, err := os.Open(args[0])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open signature file (%s): %w", args[0], err)
	}
	defer sigFile.Close()
	sig := new(bytes.Buffer)
	if _, err = io.Copy(sig, sigFile); err != nil {
		return nil, nil, fmt.Errorf("failed to read signature file: %w", err)
	}

	var dataFile io.Reader
	// Read in signed data
	if args[1] == "-" {
		dataFile = s.In
	} else {
		f2, err := os.Open(args[1])
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open message file (%s): %w", args[1], err)
		}
		defer f2.Close()
		dataFile = f2
	}
	buf := new(bytes.Buffer)
	if _, err = io.Copy(buf, dataFile); err != nil {
		return nil, nil, fmt.Errorf("failed to read message file: %w", err)
	}

	return buf.Bytes(), sig.Bytes(), nil
}
