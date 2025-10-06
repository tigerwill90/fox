

Routing priority
- Always search for the longest route (hostname and path)
- Route with hostname are evaluated before route without hostname
- Static segment that overlap with params and wildcard are evaluated in priority
- Params that overlap with wildcard are evaluated in priority
- Regexp params that overlap with other params are evaluated in priority
- Regexp wildcard that overlap with other wildcard are evaluated in priority
- Multiple regexp params that overlap are evaluated in register order
- Multiple regexp wildcard that overlap are evaluated in register order

Previously route with overlapping parameter or wildcard but with different name was not allowed, e.g. 

/users/{id}
/users/{email}/action:{action}

This is now allowed as long as both would not match the exact same route.


But for example, those route

/{name}/users
/{id}/users

or 

/users/{name}
/users/{id}

have different param names but are in fact identical, those they are not allowed

internally, fox convert those param with the same key ? for param and * for wildcard
so those route would be represented as 

/users/{?}
/users/{?}

But this is something we DON'T NEED to explain to the user, it's just for your understanding

Route can also now be registered with regexp

e.g. /users/{id:[0-9]+}

Regular expression cannot contain capturing group (pattern), but can use non capturing group (?:pattern)

It's possible to have multiple overlapping params and wildcard (and mixing it together)

For instance:

/users/{id:[0-9]+}
/users/{name}

are allowed. We could argue that this is the same case as 
/users/{id}
/users/{name}
and therfore would not be allowed

But no because, the regular expression allow to add specificity that would make to same route match different request

In the case of mixing regular expression and non regular expression, we evaluate always regular expression, therefore
param without regular expression can act as a fallback.

This is possible to have multiple regexp at the same level and can all use a different parameter name as long as the route are not stricly identical.
/users/{id:[0-9]+}
/users/{name:[A-z]+}
/users/{anything_else}

Thas also mean that we can catch
/users/{id:[0-9]+}
/users/{name:[0-9]+}

two identical route, that use different name, but have a identical regular expression, meaning we are is the same case as
/users/{id}
/users/{name}


Parameters are supported in hostname and path.

This capture a full segment (or label)
/{param}/ or /{param}
or the suffix of a segment
/id:{id}/ or /id:{id}

But they cannot be in the prefix of a segment, or in the middle of a segment.

for example

/{params}after/ or /{params}after
or
/before{params}after/

The same logic apply for wildcard, with the notable difference that wildcard are not supported in hostname.

This can capture multiple segments
/*{wildcard}/ or /*{wildcard}
or the suffix of a segment
/file:*{filepath}/ or /file*{filepath}


An important last point, wildcard and params, whatever they are infix or suffix, never capture an empty segment. The route
therefore would not match.




