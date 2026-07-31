package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalizeStatusURL(t *testing.T) {
	t.Parallel()

	canonical, statusID, handle, err := CanonicalizeStatusURL(
		"https://twitter.com/CryptoDinduz/status/123456789?ref_src=twsrc%5Etfw",
	)

	require.NoError(t, err)
	require.Equal(t, "https://x.com/CryptoDinduz/status/123456789", canonical)
	require.Equal(t, "123456789", statusID)
	require.Equal(t, "CryptoDinduz", handle)
}

func TestCanonicalizeStatusURLRejectsUnsafeOrNonStatusURL(t *testing.T) {
	t.Parallel()

	for _, candidate := range []string{
		"http://x.com/user/status/1",
		"https://example.com/user/status/1",
		"https://x.com/user",
		"https://x.com/user/status/not-a-number",
	} {
		_, _, _, err := CanonicalizeStatusURL(candidate)
		require.Error(t, err, candidate)
	}
}

func TestAccountHelpers(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateAccount("@yeonwoo1102"))
	require.Error(t, ValidateAccount("yeonwoo1102"))
	require.Equal(t, "@yeonwoo1102", NormalizeAccount(" yeonwoo1102 "))
	require.True(t, EqualAccount("@CryptoDinduz", "cryptodinduz"))
}
