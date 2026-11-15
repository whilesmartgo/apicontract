# Problem types

`WriteProblem` sends RFC 9457 problem details as `application/problem+json`. Built-in problem codes have a `type` that links to their section below. When present, `errors` lists input errors: `in` says whether it is a query parameter, a body field or a header, `name` is the name or path the client sent, and `code` is one of the field codes at the end of this page.

## malformed_query

Status 400. The query string is not valid URL encoding, a declared parameter is sent twice, a value has the wrong type, such as `limit=ten`, or cursor verification fails because the cursor is malformed, altered, bound to different inputs or cannot be decoded into the destination. Fix the request before retrying.

## invalid_query

Status 422. No malformed declared value was found, but an input breaks a rule: a parameter the route does not declare, a required parameter is missing or blank, a value is outside its range or allowed set, `to` is not after `from`, or an old and a new parameter name disagree.

## malformed_body

Status 400. The body is empty, is not valid JSON, is not a JSON object, repeats a key, nests deeper than 64 levels or has data after the object, or a field has the wrong JSON type, such as a string where a number belongs or a number with a fraction where an integer belongs. A timestamp that is not RFC 3339 with an offset is also malformed.

## invalid_body

Status 422. The body parsed, but a field breaks a rule: a field the route does not declare, a missing required field, a value outside its range, length or allowed set, too many items, or an old and a new field name that disagree.

## unsupported_media_type

Status 415. `Content-Type` is missing, malformed or neither `application/json` nor `application/*+json`. If a charset is declared, it must be UTF-8.

## body_too_large

Status 413. The body is larger than the route accepts. The default limit is 1 MiB.

## malformed_header

Status 400. A header the route reads could not be parsed, such as an `Idempotency-Key` that is sent more than once, is not one quoted string or one bare key, or is shorter than 8 or longer than 128 characters.

## internal_error

Status 500. Errors that are not a `*Problem` produce a generic response without the error's text.

## Field codes

| Code | Meaning |
| --- | --- |
| `invalid` | The value has the wrong type or format, or was sent more than once. |
| `required` | The value is missing or null. |
| `out_of_range` | The number is outside its minimum or maximum, or `to` is not after `from`. |
| `too_long` | The string has more characters than allowed. |
| `too_many` | The list has more items than allowed. |
| `unknown_value` | The value is not in the allowed set. |
| `unknown_field` | The route does not declare this parameter or field. |
| `conflict` | An old and a new name for the same input disagree. |
| `invalid_cursor` | Cursor verification or position decoding failed. |
