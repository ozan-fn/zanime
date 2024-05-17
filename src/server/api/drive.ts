import { google } from "googleapis";

export async function drives() {
    const auth = new google.auth.GoogleAuth({
        credentials: {
            client_email: "drive-261@fine-cycling-356000.iam.gserviceaccount.com",
            private_key:
                "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCuL+boB6/Px997\nXuIrnLr4c0SvyGeycEsPFMyeVJMKd+sqhB0DLN8fYFG0RqOhpEwVABYMMyiNC2hU\noAdcJiaOE70UufmHx1+yunoc9ccBoxKOSTcnk2YgRC39wy9AnEMpTjaTEngqOcL3\nX3sBRFh/VPP1W0jQJYbdTFpmnZVMApML4LlkR+o5wAkN6m/Oj0FcOGZnM4k7FXfW\nqTAzVPV5fNpMrBsy9+7XXgDOUlqKTHaXvM/NdHJsLU2pr786eCtF5nYBG13OhCkO\nHjoOjnW3Rcy8I/7MiN+cCz8a99jK6AznmG048zRTUI+aAOAFJRHMblEX5iB0eZa6\ncarpSU2JAgMBAAECggEACJAayBfOpQaEnj4/HJk/GMUj4IdXz64VBogFehgaxWY5\nRZUKjxQm4NY9lgDt/N2Ejvi63L0sFmjz50sZqYZHmW7veeQdl/btZa4koCVV8+bm\nG+AOCdBFcjsfzqyId2LnK1HovWioRx0hUENdmuWW+yV8O7jR1setflYJUKeZBgw6\niM1ptS7RCmCyvUvC8rqSEFUTN/fmTRtNQv2EGQoBm1bY5t1oteSYvUcrlUyzkdIu\n61bg+b8XvH3onLUZtxH7NVbqfLGJWq+YHJYTJmPE9+4tP27oVbMbffqoQ+kyIB6w\nBDhVDXzJyu24U1Lt33GYEO4h9mTutfbRBPSKv+3c3QKBgQDjCdF9vs/TzQcuHOVD\nIBYWU8dRLZtPB41spPPgoj44dv99eLcg10TXxa62OGOCTz/ybemqiwLbSEQyVH6r\nwETt0mWbQiTBSyVS7eSmiSVGJUJRooxs5fH1Z1H/7T6MTBQHfxA5vTIh8z3vpfuE\njf2EQI7OI0BlyVYR+OnbptTHvQKBgQDEaCtwwfkxNQWGVkWd89IKZwwvQ7dk2dU1\ns66/lLxXUDSEuEe6vxRbwJVMkxbsNooQTxuHQ255E+14A3hbl/E5+BywbMKZ35Fj\nlwXE/N2L2OFbyLWatleR8GBPzNhsobnZHeQ2wW7tfRgvysLQV1pyOuhR/ICc8Fie\ndaQzQx8jvQKBgHtc6GGk/pQdQZuCw+f3VUkIeLLZTxxgBC5WatVSEvDnZ+erYgYM\nMjH0DfyhIyC5Iwv9QQwTwJeZ6sRWOQXl0Q5QVjlIdWQ6CqvG64i52HEG6cQlyJub\nTh+D5oBjTqs442E3X3+gF89uteazj7j01h24XmAPs3wbLBNT5u4c9l0ZAoGACHsv\nFQlrR1Kw0GI9U1rKAST+A7R64OH/MPh3XVKA54xRGEIU5t2FdYjVaZ8YaE7am2Wv\nHKIuzQ+tO3d65QmkowoxwvSPWrGqx5An8Z+xuW4+HQaHwQyKr4SfxBJmbhp1ughu\naGbl8V9rgfTRgMmk4DXm/lNXAm/mg8E0u/n0CEkCgYEAuejNQpnSHUNmEypr1008\n68iX4qD2ajO6Tk9Z9Ks7Qo9XNyOpvTODy0JnL6LBvTNjkT+snUPooWJTb6C7oAZH\npeSsW7SNuQuzM+ze0zouuuC78BnCBjrJEhmHiGPZwp2PxV9lFCTMv3/5AJ4zA8wR\n/iaXzvAOxPAqpfLIY+TvviM=\n-----END PRIVATE KEY-----\n",
        },
        scopes: ["https://www.googleapis.com/auth/drive"],
    });

    const drive = google.drive({
        version: "v3",
        auth,
    });

    return drive;
}
