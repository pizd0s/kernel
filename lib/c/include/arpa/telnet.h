#pragma once

/*
 * The TELNET protocol's codes (RFC 854, 855 and the options' RFCs), as the
 * libcs have them: the commands after IAC, the options, the qualifiers of
 * a suboption, and the names of both, tables a source gets by defining
 * TELCMDS and TELOPTS before it includes this.
 */
#define IAC 255
#define DONT 254
#define DO 253
#define WONT 252
#define WILL 251
#define SB 250
#define GA 249
#define EL 248
#define EC 247
#define AYT 246
#define AO 245
#define IP 244
#define BREAK 243
#define DM 242
#define NOP 241
#define SE 240
#define EOR 239
#define ABORT 238
#define SUSP 237
#define xEOF 236
#define SYNCH 242

#define TELCMD_FIRST xEOF
#define TELCMD_LAST IAC
#define TELCMD_OK(x) ((unsigned int)(x) <= TELCMD_LAST && (unsigned int)(x) >= TELCMD_FIRST)
#define TELCMD(x) telcmds[(x) - TELCMD_FIRST]

#ifdef TELCMDS
const char* telcmds[] = {"EOF", "SUSP", "ABORT", "EOR", "SE", "NOP", "DMARK", "BRK", "IP", "AO",
                         "AYT", "EC", "EL", "GA", "SB", "WILL", "WONT", "DO", "DONT", "IAC", 0};
#else
extern const char* telcmds[];
#endif

#define TELOPT_BINARY 0
#define TELOPT_ECHO 1
#define TELOPT_RCP 2
#define TELOPT_SGA 3
#define TELOPT_NAMS 4
#define TELOPT_STATUS 5
#define TELOPT_TM 6
#define TELOPT_RCTE 7
#define TELOPT_NAOL 8
#define TELOPT_NAOP 9
#define TELOPT_NAOCRD 10
#define TELOPT_NAOHTS 11
#define TELOPT_NAOHTD 12
#define TELOPT_NAOFFD 13
#define TELOPT_NAOVTS 14
#define TELOPT_NAOVTD 15
#define TELOPT_NAOLFD 16
#define TELOPT_XASCII 17
#define TELOPT_LOGOUT 18
#define TELOPT_BM 19
#define TELOPT_DET 20
#define TELOPT_SUPDUP 21
#define TELOPT_SUPDUPOUTPUT 22
#define TELOPT_SNDLOC 23
#define TELOPT_TTYPE 24
#define TELOPT_EOR 25
#define TELOPT_TUID 26
#define TELOPT_OUTMRK 27
#define TELOPT_TTYLOC 28
#define TELOPT_3270REGIME 29
#define TELOPT_X3PAD 30
#define TELOPT_NAWS 31
#define TELOPT_TSPEED 32
#define TELOPT_LFLOW 33
#define TELOPT_LINEMODE 34
#define TELOPT_XDISPLOC 35
#define TELOPT_OLD_ENVIRON 36
#define TELOPT_AUTHENTICATION 37
#define TELOPT_ENCRYPT 38
#define TELOPT_NEW_ENVIRON 39
#define TELOPT_EXOPL 255

#define NTELOPTS (1 + TELOPT_NEW_ENVIRON)
#define TELOPT_FIRST TELOPT_BINARY
#define TELOPT_LAST TELOPT_NEW_ENVIRON
#define TELOPT_OK(x) ((unsigned int)(x) <= TELOPT_LAST)
#define TELOPT(x) telopts[(x) - TELOPT_FIRST]

#ifdef TELOPTS
const char* telopts[NTELOPTS + 1] = {
    "BINARY", "ECHO", "RCP", "SUPPRESS GO AHEAD", "NAME", "STATUS", "TIMING MARK", "RCTE", "NAOL", "NAOP", "NAOCRD", "NAOHTS",
    "NAOHTD", "NAOFFD", "NAOVTS", "NAOVTD", "NAOLFD", "EXTEND ASCII", "LOGOUT", "BYTE MACRO", "DATA ENTRY TERMINAL", "SUPDUP",
    "SUPDUP OUTPUT", "SEND LOCATION", "TERMINAL TYPE", "END OF RECORD", "TACACS UID", "OUTPUT MARKING", "TTYLOC", "3270 REGIME",
    "X.3 PAD", "NAWS", "TSPEED", "LFLOW", "LINEMODE", "XDISPLOC", "OLD-ENVIRON", "AUTHENTICATION", "ENCRYPT", "NEW-ENVIRON", 0,
};
#else
extern const char* telopts[];
#endif

#define TELQUAL_IS 0
#define TELQUAL_SEND 1
#define TELQUAL_INFO 2
#define TELQUAL_REPLY 2
#define TELQUAL_NAME 3

#define LFLOW_OFF 0
#define LFLOW_ON 1
#define LFLOW_RESTART_ANY 2
#define LFLOW_RESTART_XON 3

#define NEW_ENV_VAR 0
#define NEW_ENV_VALUE 1
#define ENV_ESC 2
#define ENV_USERVAR 3

#define OLD_ENV_VAR 1
#define OLD_ENV_VALUE 0
