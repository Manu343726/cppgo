#include "rules.hpp"

namespace rules {

Widget::Widget(int n) : n_(n) {}
Widget::~Widget() {}

int Widget::size() const { return n_; }

int Widget::area() const { return n_; }

int Widget::six(int, int, int, int, int, int) const { return n_; }

int Widget::dims() { return 2; }

int sum_pod(Pod p) { return p.a + p.b; }

std::size_t text_length(std::string s) { return s.size(); }

Pod make_pod(int a, int b) { return Pod{a, b}; }

int pick(Widget, int Widget::*) { return 0; }

int pick_one(int Widget::*) { return 0; }

long count() { return 0; }

void notify() {}

int text_bytes(const char *text, std::size_t length) {
    return static_cast<int>(text && length ? 1 : 0);
}

int over(int a) { return a; }

int over(int a, int b) { return a + b; }

int add(int a, int b) { return a + b; }

double add(double a, double b) { return a + b; }

double ratio(double a, float b) { return a * b; }

static int internal_counter() { return 0; }

namespace {
void hidden_helper() {}
} // namespace

} // namespace rules
