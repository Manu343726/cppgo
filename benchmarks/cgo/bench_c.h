/* bench_c.h — the extern "C" surface the cgo arm calls.
 *
 * This header and shim.cpp are what a project writes by hand when it reaches a
 * C++ library through cgo: one wrapper per call, every one of them catching at
 * the language boundary, because a C++ exception that unwinds into a Go frame
 * is undefined behaviour. It is deliberately shaped like the thunks cppgo
 * generates (status code, out-parameters, results copied out) so the two arms
 * do the same work and differ only in the crossing.
 *
 * The declarations here are also what cgo reads: cgo.go's preamble includes
 * this header, so C.struct_bench_point below is the C++ layout.
 */
#ifndef BENCH_C_H
#define BENCH_C_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

struct bench_point {
    double x;
    double y;
};

struct bench_sample {
    double a;
    double b;
    double c;
};

/* bench_last_error is the message the last failed wrapper on this thread
 * recorded, or "". Its storage is valid until the next wrapper call. */
const char *bench_last_error(void);

/* bench_free releases a result copy the Go side has read. */
void bench_free(void *block);

int64_t bench_add(int64_t a, int64_t b);
int32_t bench_add_thunk(int64_t a, int64_t b, int64_t *out);

int32_t bench_midpoint(struct bench_point a, struct bench_point b, struct bench_point *out);
void bench_blend(struct bench_sample a, struct bench_sample b, double t, struct bench_sample *out);

int32_t bench_describe(const char *prefix_data, size_t prefix_size, double value, void **out_data,
                       size_t *out_size);
int32_t bench_doubled(const double *values_data, size_t values_size, void **out_data,
                      size_t *out_size);

int32_t bench_ratio(int32_t a, int32_t b, int32_t *out);
int32_t bench_parse(const char *text_data, size_t text_size);

int32_t bench_fold(int32_t a, int32_t b, int32_t *out);

void *bench_counter_new(int64_t start);
void bench_counter_free(void *counter);
int64_t bench_counter_get(void *counter);
void bench_counter_bump(void *counter, int64_t by);

#ifdef __cplusplus
}
#endif

#endif /* BENCH_C_H */
