#include "staticdata.hpp"

namespace statics {

std::int32_t Counter::count = 0;
const std::int32_t Counter::limit;
std::string Counter::label = "shared";
const std::string Counter::name = "counter";

std::int32_t Counter::instances() noexcept { return 1; }

} // namespace statics
