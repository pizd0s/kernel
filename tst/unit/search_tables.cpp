//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "search tables: ok\n"

// search.h, POSIX's and GNU's: a hash table by hcreate and hsearch, its
// _r kind on a table of the program's, a binary tree by tsearch whose
// walk visits each inner node before, between and after its children and
// a leaf once, at their depths, the keys in order between; tdelete and
// tdestroy; a linear search that appends; and a queue of insque and
// remque. The tree's shape is the implementation's, so the walk is
// checked by those rules, not as a sequence.

#include <assert.h>
#include <errno.h>
#include <search.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

namespace {
    int compare(const void* a, const void* b) {
        int x = *(const int*)a;
        int y = *(const int*)b;
        return x < y ? -1 : x > y;
    }

    struct Visit {
        int key;
        VISIT order;
        int depth;
    };

    Visit visits[64];
    int count;

    void record(const void* node, VISIT order, int depth) {
        assert(count < 64);
        visits[count++] = {**(const int* const*)node, order, depth};
    }

    // The walk's rules: each inner node's preorder, postorder and endorder
    // in that order at one depth around its children's one deeper, a leaf
    // once; the keys of postorder and leaf visits ascending.
    int checked(int expected) {
        int position = 0;
        int seen = 0;
        int previous = -1;
        struct Frame {
            int key;
            int depth;
            int stage;
        } stack[64];
        int top = 0;

        for (int i = 0; i < count; i++) {
            const Visit& v = visits[i];
            if (v.order == leaf || v.order == preorder) {
                assert(v.depth == (top ? stack[top - 1].depth + 1 : 0));
            }
            if (v.order == leaf || v.order == postorder) {
                assert(v.key > previous);
                previous = v.key;
                seen++;
            }
            if (v.order == preorder) {
                stack[top++] = {v.key, v.depth, 0};
            } else if (v.order == postorder) {
                assert(top && stack[top - 1].key == v.key && stack[top - 1].depth == v.depth && stack[top - 1].stage == 0);
                stack[top - 1].stage = 1;
            } else if (v.order == endorder) {
                assert(top && stack[top - 1].key == v.key && stack[top - 1].depth == v.depth && stack[top - 1].stage == 1);
                top--;
            }
            position++;
        }
        assert(top == 0 && seen == expected);
        return position;
    }

    int freed;

    void release(void* key) {
        assert(key);
        freed++;
    }

    struct Element {
        Element* next;
        Element* previous;
        int value;
    };
}

int main() {
    char keys[100][8];
    assert(hcreate(200) != 0);
    for (int i = 0; i < 100; i++) {
        snprintf(keys[i], sizeof keys[i], "k%d", i);
        ENTRY* e = hsearch(ENTRY{keys[i], (void*)(long)i}, ENTER);
        assert(e && e->key == keys[i] && e->data == (void*)(long)i);
    }
    for (int i = 0; i < 100; i++) {
        char name[8];
        snprintf(name, sizeof name, "k%d", i);
        ENTRY* e = hsearch(ENTRY{name, nullptr}, FIND);
        assert(e && strcmp(e->key, name) == 0 && e->data == (void*)(long)i);
    }
    char absent[] = "absent";
    assert(hsearch(ENTRY{absent, nullptr}, FIND) == nullptr);
    ENTRY* again = hsearch(ENTRY{keys[7], (void*)999L}, ENTER);
    assert(again && again->data == (void*)7L);
    hdestroy();
    assert(hcreate(4) != 0);
    assert(hsearch(ENTRY{keys[1], nullptr}, FIND) == nullptr);
    hdestroy();

    struct hsearch_data table;
    memset(&table, 0, sizeof table);
    assert(hcreate_r(50, &table) != 0);
    ENTRY* found = nullptr;
    assert(hsearch_r(ENTRY{keys[3], (void*)3L}, ENTER, &found, &table) && found && found->data == (void*)3L);
    assert(hsearch_r(ENTRY{keys[3], nullptr}, FIND, &found, &table) && found->key == keys[3]);
    assert(!hsearch_r(ENTRY{absent, nullptr}, FIND, &found, &table) && found == nullptr);
    hdestroy_r(&table);

    static int values[] = {5, 3, 8, 1, 4, 7, 9, 2, 6};
    void* root = nullptr;
    for (int& value : values) {
        void* node = tsearch(&value, &root, compare);
        assert(node && *(int**)node == &value);
    }
    int five = 5;
    int ten = 10;
    void* existing = tsearch(&five, &root, compare);
    assert(existing && *(int**)existing == &values[0]);
    assert(tfind(&five, &root, compare) == existing && tfind(&ten, &root, compare) == nullptr);
    twalk(root, record);
    int inner = 0;
    for (int i = 0; i < count; i++) {
        inner += visits[i].order == preorder;
    }
    assert(checked(9) == 9 + 2 * inner);
    int root_key = **(int**)root;
    assert(tdelete(&root_key, &root, compare) != nullptr);
    assert(tdelete(&ten, &root, compare) == nullptr);
    int three = 3;
    assert(tdelete(&three, &root, compare) != nullptr && tfind(&three, &root, compare) == nullptr);
    count = 0;
    twalk(root, record);
    inner = 0;
    for (int i = 0; i < count; i++) {
        inner += visits[i].order == preorder;
    }
    assert(checked(7) == 7 + 2 * inner);
    tdestroy(root, release);
    assert(freed == 7);

    int list[8] = {4, 2, 9};
    size_t members = 3;
    int nine = 9;
    int six = 6;
    assert(lfind(&nine, list, &members, sizeof(int), compare) == &list[2]);
    assert(lfind(&six, list, &members, sizeof(int), compare) == nullptr && members == 3);
    assert(lsearch(&six, list, &members, sizeof(int), compare) == &list[3] && members == 4 && list[3] == 6);
    assert(lsearch(&nine, list, &members, sizeof(int), compare) == &list[2] && members == 4);

    Element a = {nullptr, nullptr, 1};
    Element b = {nullptr, nullptr, 2};
    Element c = {nullptr, nullptr, 3};
    insque(&a, nullptr);
    assert(a.next == nullptr && a.previous == nullptr);
    insque(&c, &a);
    insque(&b, &a);
    assert(a.next == &b && b.next == &c && c.next == nullptr && c.previous == &b && b.previous == &a);
    remque(&b);
    assert(a.next == &c && c.previous == &a);
    remque(&a);
    assert(c.previous == nullptr);

    printf("search tables: ok\n");
}
