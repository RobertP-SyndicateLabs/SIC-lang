package main

import (
    "fmt"
    "io/ioutil"
    "os"

    "github.com/RobertP-SyndicateLabs/SIC-lang/compiler"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Println("usage: sic <command> [args]")
        os.Exit(1)
    }

    cmd := os.Args[1]

    switch cmd {
    case "build":
        doBuild(os.Args[2:])
    case "run":
        doRun(os.Args[2:])
    case "fmt":
        doFmt(os.Args[2:])
    case "analyze":
        doAnalyze(os.Args[2:])
    case "lex":
        doLex(os.Args[2:])
    default:
        fmt.Println("unknown command:", cmd)
        os.Exit(1)
    }
}

func doBuild(args []string) {
    fmt.Println("[SIC] build is not implemented yet.")
}

func doRun(args []string) {
    fmt.Println("[SIC] run is not implemented yet.")
}

func doFmt(args []string) {
    fmt.Println("[SIC] fmt is not implemented yet.")
}

func doAnalyze(args []string) {
    fmt.Println("[SIC] analyze is not implemented yet.")
}

func doLex(args []string) {
    if len(args) == 0 {
        fmt.Println("usage: sic lex <file.sic>")
        os.Exit(1)
    }

    filename := args[0]
    data, err := ioutil.ReadFile(filename)
    if err != nil {
        fmt.Println("error reading file:", err)
        os.Exit(1)
    }

    src := string(data)
    lx := compiler.NewLexer(src, filename)

    for {
        tok := lx.NextToken()
        fmt.Printf("%-12s %-20q (%s:%d:%d)\n",
            tok.Type, tok.Lexeme, tok.File, tok.Line, tok.Column)

        if tok.Type == compiler.TOK_EOF {
            break
        }
        if tok.Type == compiler.TOK_ILLEGAL {
            fmt.Println("ILLEGAL token encountered, stopping.")
            break
        }
    }
}
