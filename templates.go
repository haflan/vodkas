// TODO: A better alternative to this inline HTML stuff:
//       https://odino.org/bundling-static-files-within-your-golang-app/
package main

import "html/template"


// I think template data variables must be exported (upper case) in order to be usable
var uploadPageTemplate = template.Must(template.New("uploadPage").Parse(`
<!DOCTYPE html>
<html>
<head>
    <style>
        textarea { width: 100%; }
    </style>

</head>
<body>
<h1>Upload to /{{.ShotKey}}</h1>

<form action="/{{.ShotKey}}" method="POST" onreset="formReset()" enctype="multipart/form-data">
    <label for="input-numdls">Number of downloads</label><br>
    <input value="1" type="number" id="input-numdls" name="numdls"><br>
    <p>
        <label for="input-file">File dump</label><br>
        <input type="file" id="input-file" name="file" style="width: 100%;"
               oninput="inputChanged(this, '#input-text')"><br>
        <b>or</b><br>
        <label for="input-text">Text dump</label><br>
        <textarea id="input-text" name="text" rows="15"
                  oninput="inputChanged(this, '#input-file')"></textarea><br>
    </p>
    <input type="reset" value="Reset">
    <input type="submit" value="Submit">
</form>

<script>
    function formReset() {
        document.querySelector("#input-text").removeAttribute("disabled");
        document.querySelector("#input-file").removeAttribute("disabled");
    }
    function inputChanged(changedInput, otherInputId) {
        const otherInput = document.querySelector(otherInputId);
        if (changedInput.value) {
            otherInput.setAttribute("disabled", true);
        } else {
            otherInput.removeAttribute("disabled");
        }
    }
</script>
</body>
</html>
`))
