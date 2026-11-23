#pragma once

/*
 * The FTP protocol's codes (RFC 959), as the libcs have them: the first
 * digit of a reply, the representation types, formats, structures and
 * modes of a transfer, the escapes of a record's and the flags of a
 * block's, and the names of the types, formats, structures and modes, the
 * tables a source gets by defining FTP_NAMES before it includes this.
 */
#define PRELIM 1
#define COMPLETE 2
#define CONTINUE 3
#define TRANSIENT 4
#define ERROR 5

#define TYPE_A 1
#define TYPE_E 2
#define TYPE_I 3
#define TYPE_L 4

#define FORM_N 1
#define FORM_T 2
#define FORM_C 3

#define STRU_F 1
#define STRU_R 2
#define STRU_P 3

#define MODE_S 1
#define MODE_B 2
#define MODE_C 3

#define REC_ESC '\377'
#define REC_EOR '\001'
#define REC_EOF '\002'

#define BLK_EOR 0x80
#define BLK_EOF 0x40
#define BLK_ERRORS 0x20
#define BLK_RESTART 0x10
#define BLK_BYTECOUNT 2

#ifdef FTP_NAMES
const char* modenames[] = {"0", "Stream", "Block", "Compressed"};
const char* strunames[] = {"0", "File", "Record", "Page"};
const char* typenames[] = {"0", "ASCII", "EBCDIC", "Image", "Local"};
const char* formnames[] = {"0", "Nonprint", "Telnet", "Carriage-control"};
#endif
