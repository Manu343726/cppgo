// A header with no system includes: libclang completes the explicit
// instantiation here, which is the case where a specialisation is mirrorable.
namespace spec {

template <typename T>
struct Box {
    T value;
    T get() const { return value; }
};
template struct Box<int>;

struct Holder {
    Box<int> box;
};

} // namespace spec
