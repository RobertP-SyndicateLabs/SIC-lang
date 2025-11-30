package main

import (
    "fmt"
    "io/ioutil"
    "os"
    "strings"

    "github.com/RobertP-SyndicateLabs/SIC-lang/compiler"
)

func findCommand(args []string) (string, int) {
    // Skip path-like arguments: /something/something OR ./something
    for i, a := range args {
        if strings.HasPrefix(a, "/") || strings.HasPrefix(a, "./") {
            continue
        }
        return a, i
    }
    return "", -1
}

func main() {
    if len(os.Args) < 2 {
        fmt.Println("usage: sic <command> [args]")
        os.Exit(1)
    }

    cmd, idx := findCommand(os.Args[1:])
    if idx == -1 {
        fmt.Println("no valid command found")
        os.Exit(1)
    }

    // real arguments start AFTER the command
    args := os.Args[1+idx+1:]

    switch cmd {
    case "build":
        doBuild(args)
    case "run":
        doRun(args)
    case "fmt":
        doFmt(args)
    case "analyze":
        doAnalyze(args)
    case "lex":
        doLex(args)
    case "parse":
        doParse(args)
    default:
        fmt.Println("unknown command:", cmd)
        os.Exit(1)
    }
}

func doBuild(args []string) {
    fmt.Println("[SIC] build is not implemented yet.")
}

func doRun(args []string) {
    if len(args) == 0 {
        fmt.Println("usage: sic run <file.sic>")
        os.Exit(1)
    }

    filename := args[0]

    if err := compiler.RunFile(filename); err != nil {
        fmt.Fprintln(os.Stderr, "[SIC] runtime error:", err)
        os.Exit(1)
    }
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

func doParse(args []string) {
    if len(args) == 0 {
        fmt.Println("usage: sic parse <file.sic>")
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
    p := compiler.NewParser(lx)
    prog := p.ParseProgram()

    if errs := p.Errors(); len(errs) > 0 {
        fmt.Println("Parser reported errors:")
        for _, e := range errs {
            fmt.Println("  -", e)
        }
        os.Exit(1)
    }

    fmt.Println("== SIC PROGRAM ==")
    fmt.Println("Language:", prog.Language)
    fmt.Println("Scroll:", prog.Scroll)
    fmt.Println("Mode:", prog.Mode)
    fmt.Println("Profile:", prog.Profile)
    fmt.Println("Works:")
    for _, w := range prog.Works {
        fmt.Printf("  - %s (tokens in body: %d)\n", w.Name, len(w.Body))
    }
}
