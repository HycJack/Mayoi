# The terminal's native library

`ghostty-vt.dll` is libghostty-vt for windows/amd64: the parser the terminal draws with, built into
`Mayoi.exe` by `natives_windows_amd64.go`, and unpacked to the user's cache on first start so that a
`Mayoi.exe` copied on its own still opens a terminal. A library already beside the executable, or one
named by `MYGO_GHOSTTY_VT`, is used instead.

It is not built here. It is the release that the MyGo version in `go.mod` binds, named by that
version's plugin manifest:

    go env GOMODCACHE → github.com/sorrycc/mygo@<version>/plugins/terminal/mygo-plugin.json
    libraries[0].files["windows-amd64"] → { name, sha256, url }

To update it, on a MyGo bump: take the `windows-amd64` entry from the new manifest, download its
`url`, check the `sha256`, replace this file, and put the new hash in `ghosttySHA` in
`cmd/mayoi/natives_test.go`. The test fails until both match, which is the point: the terminal draws
with the library it was built against, not with whichever one a folder happened to keep.
