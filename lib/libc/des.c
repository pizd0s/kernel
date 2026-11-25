/*
 * setkey and encrypt, XSI's DES of one 64-bit block, a char a bit, the
 * low bit of each: setkey's key, its parity bits unused, is the process's
 * for every encrypt after; encrypt enciphers the block in place, or with
 * a flag deciphers it. DES as FIPS 46 has it, bit by bit, each table's
 * entries the bits, from 1, its result takes.
 */
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static const unsigned char initial[64] = {
    58, 50, 42, 34, 26, 18, 10, 2, 60, 52, 44, 36, 28, 20, 12, 4, 62, 54, 46, 38, 30, 22, 14, 6, 64, 56, 48, 40, 32, 24, 16, 8,
    57, 49, 41, 33, 25, 17, 9,  1, 59, 51, 43, 35, 27, 19, 11, 3, 61, 53, 45, 37, 29, 21, 13, 5, 63, 55, 47, 39, 31, 23, 15, 7,
};

static const unsigned char choice1[56] = {
    57, 49, 41, 33, 25, 17, 9,  1,  58, 50, 42, 34, 26, 18, 10, 2,  59, 51, 43, 35, 27, 19, 11, 3,  60, 52, 44, 36,
    63, 55, 47, 39, 31, 23, 15, 7,  62, 54, 46, 38, 30, 22, 14, 6,  61, 53, 45, 37, 29, 21, 13, 5,  28, 20, 12, 4,
};

static const unsigned char choice2[48] = {
    14, 17, 11, 24, 1,  5,  3,  28, 15, 6,  21, 10, 23, 19, 12, 4,  26, 8,  16, 7,  27, 20, 13, 2,
    41, 52, 31, 37, 47, 55, 30, 40, 51, 45, 33, 48, 44, 49, 39, 56, 34, 53, 46, 42, 50, 36, 29, 32,
};

static const unsigned char shifts[16] = {1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1};

static const unsigned char expansion[48] = {
    32, 1,  2,  3,  4,  5,  4,  5,  6,  7,  8,  9,  8,  9,  10, 11, 12, 13, 12, 13, 14, 15, 16, 17,
    16, 17, 18, 19, 20, 21, 20, 21, 22, 23, 24, 25, 24, 25, 26, 27, 28, 29, 28, 29, 30, 31, 32, 1,
};

static const unsigned char permutation[32] = {
    16, 7, 20, 21, 29, 12, 28, 17, 1, 15, 23, 26, 5, 18, 31, 10, 2, 8, 24, 14, 32, 27, 3, 9, 19, 13, 30, 6, 22, 11, 4, 25,
};

static const unsigned char boxes[8][64] = {
    {14, 4,  13, 1, 2,  15, 11, 8,  3,  10, 6,  12, 5,  9,  0, 7, 0, 15, 7,  4,  14, 2,  13, 1,  10, 6, 12, 11, 9,  5,  3,  8,
     4,  1,  14, 8, 13, 6,  2,  11, 15, 12, 9,  7,  3,  10, 5, 0, 15, 12, 8,  2,  4,  9,  1,  7,  5,  11, 3, 14, 10, 0,  6, 13},
    {15, 1,  8,  14, 6,  11, 3,  4,  9,  7, 2,  13, 12, 0, 5,  10, 3,  13, 4,  7,  15, 2,  8,  14, 12, 0, 1,  10, 6,  9,  11, 5,
     0,  14, 7,  11, 10, 4,  13, 1,  5,  8, 12, 6,  9,  3, 2,  15, 13, 8,  10, 1,  3,  15, 4,  2,  11, 6, 7,  12, 0,  5,  14, 9},
    {10, 0,  9,  14, 6, 3,  15, 5,  1,  13, 12, 7,  11, 4,  2,  8,  13, 7,  0,  9,  3,  4,  6,  10, 2,  8, 5,  14, 12, 11, 15, 1,
     13, 6,  4,  9,  8, 15, 3,  0,  11, 1,  2,  12, 5,  10, 14, 7,  1,  10, 13, 0,  6,  9,  8,  7,  4,  15, 14, 3,  11, 5,  2,  12},
    {7,  13, 14, 3, 0,  6,  9,  10, 1,  2, 8, 5,  11, 12, 4,  15, 13, 8,  11, 5, 6,  15, 0,  3,  4,  7,  2,  12, 1,  10, 14, 9,
     10, 6,  9,  0, 12, 11, 7,  13, 15, 1, 3, 14, 5,  2,  8,  4,  3,  15, 0,  6, 10, 1,  13, 8,  9,  4,  5,  11, 12, 7,  2,  14},
    {2,  12, 4,  1,  7,  10, 11, 6,  8,  5,  3,  15, 13, 0, 14, 9,  14, 11, 2,  12, 4,  7,  13, 1,  5,  0,  15, 10, 3,  9, 8,  6,
     4,  2,  1,  11, 10, 13, 7,  8,  15, 9,  12, 5,  6,  3, 0,  14, 11, 8,  12, 7,  1,  14, 2,  13, 6,  15, 0,  9,  10, 4, 5,  3},
    {12, 1,  10, 15, 9, 2,  6,  8,  0,  13, 3,  4,  14, 7,  5,  11, 10, 15, 4,  2,  7,  12, 9,  5,  6,  1,  13, 14, 0,  11, 3, 8,
     9,  14, 15, 5,  2, 8,  12, 3,  7,  0,  4,  10, 1,  13, 11, 6,  4,  3,  2,  12, 9,  5,  15, 10, 11, 14, 1,  7,  6,  0,  8, 13},
    {4,  11, 2,  14, 15, 0,  8,  13, 3,  12, 9,  7,  5,  10, 6, 1,  13, 0,  11, 7,  4,  9,  1,  10, 14, 3,  5,  12, 2,  15, 8, 6,
     1,  4,  11, 13, 12, 3,  7,  14, 10, 15, 6,  8,  0,  5,  9, 2,  6,  11, 13, 8,  1,  4,  10, 7,  9,  5,  0,  15, 14, 2,  3, 12},
    {13, 2,  8,  4, 6,  15, 11, 1,  10, 9,  3,  14, 5,  0,  12, 7,  1,  15, 13, 8,  10, 3,  7,  4,  12, 5,  6,  11, 0,  14, 9,  2,
     7,  11, 4,  1, 9,  12, 14, 2,  0,  6,  10, 13, 15, 3,  5,  8,  2,  1,  14, 7,  4,  10, 8,  13, 15, 12, 9,  0,  3,  5,  6,  11},
};

// The subkeys of setkey's key, a bit a char.
static unsigned char keys[16][48];

static void permute(unsigned char* out, const unsigned char* in, const unsigned char* table, int n) {
    for (int i = 0; i < n; i++) {
        out[i] = in[table[i] - 1];
    }
}

void __kernel_setkey(const char* key) {
    unsigned char bits[64], halves[56];

    for (int i = 0; i < 64; i++) {
        bits[i] = key[i] & 1;
    }

    permute(halves, bits, choice1, 56);

    for (int round = 0; round < 16; round++) {
        for (int s = 0; s < shifts[round]; s++) {
            for (int half = 0; half < 56; half += 28) {
                unsigned char first = halves[half];

                memmove(halves + half, halves + half + 1, 27);
                halves[half + 27] = first;
            }
        }

        permute(keys[round], halves, choice2, 48);
    }
}

void __kernel_encrypt(char* block, int decrypt) {
    unsigned char bits[64], data[64];

    for (int i = 0; i < 64; i++) {
        bits[i] = block[i] & 1;
    }

    permute(data, bits, initial, 64);

    unsigned char* left = data;
    unsigned char* right = data + 32;

    for (int round = 0; round < 16; round++) {
        const unsigned char* key = keys[decrypt ? 15 - round : round];
        unsigned char wide[48], boxed[32], mixed[32];

        permute(wide, right, expansion, 48);

        for (int i = 0; i < 48; i++) {
            wide[i] ^= key[i];
        }

        for (int b = 0; b < 8; b++) {
            const unsigned char* w = wide + 6 * b;
            int v = boxes[b][(w[0] << 5 | w[5] << 4) | (w[1] << 3 | w[2] << 2 | w[3] << 1 | w[4])];

            for (int i = 0; i < 4; i++) {
                boxed[4 * b + i] = v >> (3 - i) & 1;
            }
        }

        permute(mixed, boxed, permutation, 32);

        for (int i = 0; i < 32; i++) {
            unsigned char r = left[i] ^ mixed[i];

            left[i] = right[i];
            right[i] = r;
        }
    }

    // The last round's halves are not swapped, and the final permutation
    // undoes the initial one.
    for (int i = 0; i < 32; i++) {
        unsigned char r = left[i];

        left[i] = right[i];
        right[i] = r;
    }

    for (int i = 0; i < 64; i++) {
        block[initial[i] - 1] = data[i];
    }
}
