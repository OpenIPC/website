/* A vendor library's stand-in: built without unwind tables and stripped, so
 * nothing but its dynamic symbols names a frame in it. */
#include <string.h>

int toy_lib_fault(const char *s) {
    volatile const char *p = s;
    int first = p[0];
    return first + (int)strlen(s);
}
