package elfdeps

import "strings"

// IsRuntime reports whether a DT_NEEDED name is part of the Linux/C/C++ runtime
// every program links against.
//
// The runtime is what a deployment does not carry: libc and the loader, the
// compiler's support libraries, and the C++ standard library. Everything else —
// a library the program's own C++ side needs — is something the user has to
// decide about, which is the classification the packaging choice is made on.
//
// The test is on the name, because a dependency that did not resolve has no file
// to inspect, and because the set is defined by what it is, not by where a
// distribution happened to install it.
func IsRuntime(name string) bool {
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}

	for _, prefix := range []string{
		// The loader and the C library.
		"ld-linux", "ld64.so", "linux-vdso", "libc.so", "libm.so", "libmvec.so",
		"libdl.so", "libpthread.so", "librt.so", "libresolv.so", "libutil.so",
		"libnsl.so", "libcrypt.so", "libanl.so", "libBrokenLocale.so",
		"libthread_db.so",
		// The compiler's support libraries.
		"libgcc_s.so", "libgcc.so", "libatomic.so", "libgomp.so",
		"libquadmath.so", "libitm.so",
		"libasan.so", "libubsan.so", "liblsan.so", "libtsan.so",
		// The C++ standard library.
		"libstdc++.so", "libc++.so", "libc++abi.so", "libunwind.so",
	} {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}

	return false
}
