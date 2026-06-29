#include <stdio.h>
#include <stdlib.h>
#include <locale.h>

int main() {
    setlocale(LC_ALL, "de_DE.UTF-8");
    char *endptr;
    double d = strtod("0.000333", &endptr);
    printf("d=%f, endptr=%s\n", d, endptr);
    return 0;
}
