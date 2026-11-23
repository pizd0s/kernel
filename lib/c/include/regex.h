#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * regex, POSIX's: basic and extended expressions, with GNU's escapes
 * beside (\w \W \s \S \b \B \< \> \` \', and \? \+ \| of a basic one). A
 * regex_t is the count of an expression's groups and the kernel's place of
 * what regcomp made of it. REG_STARTEND, the BSDs' and GNU's, matches in
 * the part of the string the first regmatch_t names. The codes after
 * REG_BADRPT are the BSDs', which regcomp and regexec give too: an empty
 * expression, an error of the engine's own, an argument that is none and
 * bytes that are no character. POSIX.1-2024's REG_MINIMAL is not here: the
 * engine has no shortest match.
 */
typedef ssize_t regoff_t;

typedef struct {
    size_t re_nsub;
    void* __compiled;
} regex_t;

typedef struct {
    regoff_t rm_so;
    regoff_t rm_eo;
} regmatch_t;

#define REG_EXTENDED 1
#define REG_ICASE 2
#define REG_NOSUB 4
#define REG_NEWLINE 8

#define REG_NOTBOL 1
#define REG_NOTEOL 2
#define REG_STARTEND 4

#define REG_NOMATCH 1
#define REG_BADPAT 2
#define REG_ECOLLATE 3
#define REG_ECTYPE 4
#define REG_EESCAPE 5
#define REG_ESUBREG 6
#define REG_EBRACK 7
#define REG_EPAREN 8
#define REG_EBRACE 9
#define REG_BADBR 10
#define REG_ERANGE 11
#define REG_ESPACE 12
#define REG_BADRPT 13
#define REG_EMPTY 14
#define REG_ASSERT 15
#define REG_INVARG 16
#define REG_ILLSEQ 17

int regcomp(regex_t* __restrict, const char* __restrict, int);
int regexec(const regex_t* __restrict, const char* __restrict, size_t, regmatch_t* __restrict, int);
void regfree(regex_t*);
size_t regerror(int, const regex_t* __restrict, char* __restrict, size_t);

__END_DECLS
