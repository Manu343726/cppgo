// Package matrices names the concrete Eigen specializations this module
// instantiates.
//
// A C++ class template only exists where a type argument is supplied, and the
// Go program is where that happens: `cppgo build -scan ./matrices` loads this
// package, finds every instantiation of the generated generic API, and writes
// one thunk per member into the shim, where each call instantiates exactly the
// member it uses.
//
// The instantiations are named once, here, rather than in the tests, for a
// reason the scan makes visible: the pattern must name a package other than the
// generated one. The generated package's own constructor body instantiates
// Matrix with its type parameters (`&Matrix[_Scalar, …]`), and the scan reads
// that as an instantiation whose arguments are types rather than values; the
// parent package is a dependency of this one, not a scan target, so only the
// concrete arguments below are collected.
//
// Matrix's parameters after the scalar are Eigen's Rows, Cols, Options,
// MaxRows and MaxCols. Eigen's default storage is Options = 0, and the generic
// API can spell the dimension values it generates as D1..D8 and Dynamic (-1)
// only, so Options 0 has no Go spelling: RowMajor (D1) is the option that
// exists. README.md records the limitation and what it costs.
package matrices

import "github.com/Manu343726/cppgo/examples/eigen"

// Matrix2d is Eigen::Matrix<double, 2, 2, 1, 2, 2>: a fixed 2×2 row-major
// matrix of doubles.
type Matrix2d = eigen.Matrix[float64, eigen.D2, eigen.D2, eigen.D1, eigen.D2, eigen.D2]

// MatrixXd is Eigen::Matrix<double, -1, -1, 1, -1, -1>: dynamic rows and
// columns, row-major. It is the dynamic matrix the issue's matrix asks for, as
// close as the generic API can name one.
type MatrixXd = eigen.Matrix[float64, eigen.Dynamic, eigen.Dynamic, eigen.D1, eigen.Dynamic, eigen.Dynamic]

// NewMatrix2d constructs the fixed 2×2 specialization.
func NewMatrix2d() (*Matrix2d, error) {
	return eigen.NewMatrix[float64, eigen.D2, eigen.D2, eigen.D1, eigen.D2, eigen.D2]()
}

// NewMatrixXd constructs the dynamic specialization.
func NewMatrixXd() (*MatrixXd, error) {
	return eigen.NewMatrix[float64, eigen.Dynamic, eigen.Dynamic, eigen.D1, eigen.Dynamic, eigen.Dynamic]()
}
