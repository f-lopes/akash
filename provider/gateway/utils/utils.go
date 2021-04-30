package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/libs/log"

	ctypes "github.com/ovrclk/akash/x/cert/types"
)

func NewServerTLSConfig(ctx context.Context, certs []tls.Certificate, cquery ctypes.QueryClient) (*tls.Config, error) {
	// InsecureSkipVerify is set to true due to inability to use normal TLS verification
	// certificate validation and authentication performed in VerifyPeerCertificate
	cfg := &tls.Config{
		Certificates:       certs,
		ClientAuth:         tls.RequestClientCert,
		InsecureSkipVerify: true, // nolint: gosec
		MinVersion:         tls.VersionTLS13,
		VerifyPeerCertificate: func(certificates [][]byte, _ [][]*x509.Certificate) error {
			if len(certificates) > 0 {
				if len(certificates) != 1 {
					return errors.Errorf("tls: invalid certificate chain")
				}

				cert, err := x509.ParseCertificate(certificates[0])
				defer func() {
					if err != nil {
						ctx.Value("log").(log.Logger).Error("VerifyPeerCertificate failed", "err", err.Error())
					}
				}()

				if err != nil {
					err = errors.Wrap(err, "tls: failed to parse certificate")
					return err
				}

				// validation
				var owner sdk.Address
				if owner, err = sdk.AccAddressFromBech32(cert.Subject.CommonName); err != nil {
					err = errors.Wrap(err, "tls: invalid certificate's subject common name")
					return err
				}

				// 1. CommonName in issuer and Subject must match and be as Bech32 format
				if cert.Subject.CommonName != cert.Issuer.CommonName {
					err = errors.Wrap(err, "tls: invalid certificate's issuer common name")
					return err
				}

				// 2. serial number must be in
				if cert.SerialNumber == nil {
					err = errors.Wrap(err, "tls: invalid certificate serial number")
					return err
				}

				// 3. look up certificate on chain
				var resp *ctypes.QueryCertificatesResponse
				resp, err = cquery.Certificates(
					ctx,
					&ctypes.QueryCertificatesRequest{
						Filter: ctypes.CertificateFilter{
							Owner:  owner.String(),
							Serial: cert.SerialNumber.String(),
							State:  "valid",
						},
					},
				)
				if err != nil {
					err = errors.Wrap(err, "tls: unable to fetch certificate from chain")
					return err
				}
				if (len(resp.Certificates) != 1) || !resp.Certificates[0].Certificate.IsState(ctypes.CertificateValid) {
					err = errors.New("tls: attempt to use non-existing or revoked certificate")
					return err
				}

				clientCertPool := x509.NewCertPool()
				clientCertPool.AddCert(cert)

				opts := x509.VerifyOptions{
					Roots:                     clientCertPool,
					CurrentTime:               time.Now(),
					KeyUsages:                 []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
					MaxConstraintComparisions: 0,
				}

				if _, err = cert.Verify(opts); err != nil {
					err = errors.Wrap(err, "tls: unable to verify certificate")
					return err
				}
			}
			return nil
		},
	}

	return cfg, nil
}
